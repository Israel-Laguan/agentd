package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

const goldenEventType = "GOLDEN_CHECK_FAILURE"

type goldenCycle struct {
	Cycle    int    `json:"cycle"`
	Best     int    `json:"best"`
	Stall    int    `json:"stall"`
	Feedback string `json:"feedback"`
}

type goldenCheckedKey struct{}

// checkGoldenCriteria executes every check and records failed cycles before
// requeueing, so both the limits and feedback survive a daemon restart.
func (w *Worker) checkGoldenCriteria(ctx context.Context, task models.Task) bool {
	if len(task.GoldenCriteria) == 0 || ctx.Value(goldenCheckedKey{}) == task.ID {
		return true
	}
	if err := models.ValidateGoldenCriteria(task.SuccessCriteria, task.GoldenCriteria); err != nil {
		w.failTerminal(ctx, task, fmt.Errorf("invalid golden checks: %w", err), models.TaskStateFailedRequiresHuman)
		return false
	}
	project, err := w.store.GetProject(ctx, task.ProjectID)
	if err != nil || project == nil || strings.TrimSpace(project.WorkspacePath) == "" || w.sandbox == nil {
		w.failTerminal(ctx, task, fmt.Errorf("golden checks unavailable: project workspace or sandbox unavailable: %v", err), models.TaskStateFailedRequiresHuman)
		return false
	}
	events, err := w.store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		w.requeue(ctx, task, fmt.Sprintf("read golden check history: %v", err))
		return false
	}
	previous := lastGoldenCycle(events)
	passed, failures := w.runGoldenChecks(ctx, task, *project)
	if len(failures) == 0 {
		return true
	}
	cycle := goldenCycle{Cycle: previous.Cycle + 1, Best: previous.Best, Stall: previous.Stall, Feedback: strings.Join(failures, "\n\n")}
	if previous.Cycle == 0 || passed > previous.Best {
		cycle.Best = passed
		cycle.Stall = 0
	} else {
		cycle.Stall++
	}
	payload, _ := json.Marshal(cycle)
	liveCtx := ctx
	if ctx.Err() != nil {
		liveCtx = context.WithoutCancel(ctx)
	}
	if err := w.store.AppendEvent(liveCtx, models.Event{ProjectID: task.ProjectID, TaskID: sql.NullString{String: task.ID, Valid: true}, Type: goldenEventType, Payload: string(payload)}); err != nil {
		w.failTerminal(liveCtx, task, fmt.Errorf("persist golden check failure: %w", err), models.TaskStateFailedRequiresHuman)
		return false
	}
	if cycle.Cycle >= w.goldenCycleCap || cycle.Stall >= w.goldenStallCap {
		w.failTerminal(ctx, task, fmt.Errorf("golden checks stopped after %d cycles (%d stalled): %s", cycle.Cycle, cycle.Stall, cycle.Feedback), models.TaskStateFailedRequiresHuman)
	} else {
		w.requeue(ctx, task, "golden checks failed; feedback will be included in the next attempt")
	}
	return false
}

func (w *Worker) runGoldenChecks(ctx context.Context, task models.Task, project models.Project) (int, []string) {
	passed := 0
	var failures []string
	for _, check := range task.GoldenCriteria {
		if verdict, reason := agenthooks.EvaluateCommand(check.CheckCommand); verdict == agenthooks.CommandDeny {
			failures = append(failures, fmt.Sprintf("Criterion: %s\nCheck: %s\nOutput:\n%s", check.Criterion, check.CheckCommand, reason))
			continue
		}
		result, runErr := w.sandbox.Execute(ctx, sandbox.Payload{TaskID: task.ID, ProjectID: task.ProjectID,
			WorkspacePath: project.WorkspacePath, Command: check.CheckCommand,
			EnvVars: agenttools.BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv), WallTimeout: w.sandboxWallTimeout})
		if runErr == nil && result.Success && result.ExitCode == 0 && !result.TimedOut && result.Err() == nil {
			passed++
			continue
		}
		output := strings.TrimSpace(strings.Join([]string{result.Stdout, result.Stderr, result.FatalError}, "\n"))
		if runErr != nil {
			output += "\n" + runErr.Error()
		}
		output = strings.TrimSpace(output)
		if output == "" {
			output = strings.TrimSpace(result.Output)
		}
		if output == "" {
			output = fmt.Sprintf("exit code %d", result.ExitCode)
		}
		if w.sandboxScrubber != nil {
			output = w.sandboxScrubber.Scrub(output)
		}
		failures = append(failures, fmt.Sprintf("Criterion: %s\nCheck: %s\nOutput:\n%s", check.Criterion, check.CheckCommand, truncate(output, 4000)))
	}
	return passed, failures
}

func lastGoldenCycle(events []models.Event) goldenCycle {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "GOLDEN_CRITERIA_RESET" {
			return goldenCycle{}
		}
		if events[i].Type == goldenEventType {
			var cycle goldenCycle
			if json.Unmarshal([]byte(events[i].Payload), &cycle) == nil {
				return cycle
			}
		}
	}
	return goldenCycle{}
}

// goldenRepairPending keeps a rejected task on an execution path that can
// change its workspace and receive the previous check output.
func (w *Worker) goldenRepairPending(ctx context.Context, task models.Task) bool {
	if len(task.GoldenCriteria) == 0 {
		return false
	}
	events, err := w.store.ListEventsByTask(ctx, task.ID)
	return err != nil || lastGoldenCycle(events).Cycle > 0
}

func (w *Worker) appendGoldenFeedback(ctx context.Context, task models.Task, messages []gateway.PromptMessage) []gateway.PromptMessage {
	events, err := w.store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		return messages
	}
	if feedback := lastGoldenCycle(events).Feedback; feedback != "" {
		messages = append(messages, gateway.PromptMessage{Role: "user", Section: gateway.SectionComments, Content: "The controller's golden checks failed on the previous attempt. Fix the failures:\n" + agenthooks.WrapExternalContent("golden check output", feedback)})
	}
	return messages
}
