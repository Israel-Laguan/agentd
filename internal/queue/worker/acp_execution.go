package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"agentd/internal/agent/acp"
	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
	"agentd/internal/models"
	"agentd/internal/stage"
)

// acpExecution is what an acp agent reports for exec.turn.
type acpExecution struct {
	Summary string `json:"summary"`
}

// SchemaHint describes the acp execution contract's expected output.
func (acpExecution) SchemaHint() string {
	const intro = "A JSON object whose summary is committed as the task's result: "
	const shape = `{"summary": "what you changed and how you checked it"}`
	return intro + shape
}

// Validate checks the acp execution's reported summary.
func (e acpExecution) Validate() error {
	if strings.TrimSpace(e.Summary) == "" {
		return errors.New("summary is required")
	}
	return nil
}

// executionAccess is the workspace access an execution stage runs with: a task
// that changes nothing runs read-only even on a writable agent (SP-029), and a
// read-only agent that is handed a task that changes the workspace cannot do it.
func (w *Worker) executionAccess(ctx context.Context, task models.Task, profile models.AgentProfile) (stage.WorkspaceAccess, bool) {
	if profile.WorkspaceReadonly && task.RequiresWorkspaceChange {
		w.failTerminal(ctx, task, stage.ErrUnverifiedCompletion, models.TaskStateFailedRequiresHuman)
		return stage.AccessNone, false
	}
	if !task.RequiresWorkspaceChange {
		return stage.AccessReadOnly, true
	}
	return stage.AccessWrite, true
}

// runACPExecution runs the task on the acp agent its exec.turn route names and
// applies the outcome as the builtin loop's is applied: a completion is
// committed, a question goes to a human, an unverified completion fails to a
// human (B-082), and any other error takes the gateway error path (breaker,
// defer or retry).
func (w *Worker) runACPExecution(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, resolved executionResolution) {
	audit := w.beginTaskAudit(task, project, route.KindACP)
	succeeded := false
	ctx, cancel, done := w.registerRunCancel(ctx, task.ID)
	defer cancel()
	defer done()
	defer func() {
		audit.failed = !succeeded && !audit.suspended
		w.finishTaskAudit(task, project, route.KindACP, audit)
	}()
	access, ok := w.executionAccess(ctx, task, profile)
	if !ok {
		return
	}
	ctx = gateway.WithTaskID(ctx, task.ID)
	taskHooks, _ := w.MountAgenticHooks(project, profile)
	if err := w.RunSessionStart(ctx, task, project, taskHooks); err != nil {
		w.FailHard(ctx, task, err)
		return
	}
	clarified, blocked, err := w.RunPreTaskElicitation(ctx, task, project)
	if err != nil {
		w.FailHard(ctx, task, err)
		return
	}
	if blocked {
		audit.suspended = true
		return
	}
	task = clarified
	req, err := w.acpExecutionRequest(ctx, task, project, profile, access)
	if err != nil {
		w.FailHard(ctx, task, err)
		return
	}
	res, err := w.callACP(ctx, task, project, profile, resolved.route, resolved.agent, req, taskHooks, true)
	w.RecordTaskTokenUsage(ctx, task, res.TokenUsage, res.UsageDetails)
	if !w.saveACPHandoff(ctx, task, project, res, err) {
		return
	}
	succeeded = w.applyACPExecutionResult(ctx, task, project, profile, res, err, audit)
	if succeeded {
		w.clearAttemptHandoff(ctx, task)
	}
	audit.reviewHandoff = succeeded && profile.RequireReview
}

func (w *Worker) applyACPExecutionResult(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, res stage.StageResult, err error, audit *taskAudit) bool {
	switch {
	case errors.Is(err, acp.ErrSuspended):
		// An approval gate blocked the task on a human; it runs again once answered.
		audit.suspended = true
		return false
	case errors.Is(err, stage.ErrUnverifiedCompletion):
		w.failTerminal(ctx, task, err, models.TaskStateFailedRequiresHuman)
	case err != nil:
		w.HandleGatewayError(ctx, task, err)
	case res.Outcome == stage.OutcomeNeedsHuman:
		if err := handOffNeedsHuman(ctx, w.AskHuman, task, gateway.PurposeExecTurn, res.Output); !errors.Is(err, stage.ErrNeedsHuman) {
			w.FailHard(ctx, task, err)
		} else {
			audit.suspended = true
		}
	default:
		var out acpExecution
		_ = json.Unmarshal(res.Output, &out) // the contract has validated it
		return w.commitExecution(ctx, task, project, profile, out.Summary)
	}
	return false
}

// saveACPHandoff stores the note before the result is applied, which may make
// the task READY for a retry. It reports false once it has failed the task.
func (w *Worker) saveACPHandoff(ctx context.Context, task models.Task, project models.Project, res stage.StageResult, err error) bool {
	if errors.Is(err, acp.ErrSuspended) || errors.Is(err, errStageContextOverBudget) || res.Outcome == stage.OutcomeNeedsHuman {
		return true
	}
	if err == nil {
		err = errors.New("attempt awaiting coordinator checks; see check feedback if it was rejected")
	} else if neverStarted(res) && w.keepsOlderHandoff(ctx, task) {
		// An attempt that failed before the agent did anything (no runtime, jail
		// down, a guard) has nothing to hand over. Its note would be the newest
		// and shadow the last real one (B-095); the failure is in the ERROR path.
		return true
	}
	if handoffErr := w.writeAttemptHandoff(ctx, task, res, err); handoffErr != nil {
		liveCtx, cancel := liveContext(ctx)
		defer cancel()
		w.failTerminal(liveCtx, task, fmt.Errorf("persist attempt handoff: %w", handoffErr), models.TaskStateFailedRequiresHuman)
		return false
	}
	return true
}

func (w *Worker) acpExecutionRequest(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, access stage.WorkspaceAccess) (none stage.StageRequest, err error) {
	messages, err := w.buildStageContext(ctx, task, project, profile)
	if err != nil {
		return none, err
	}
	return stage.StageRequest{
		Stage:    stage.StageExecution,
		Purpose:  gateway.PurposeExecTurn,
		Context:  messages,
		Contract: stage.NewContract[acpExecution](),
		Access:   access,
		AgentID:  task.AgentID,
		Attrs:    gateway.AttrsForTask(task.RequiresWorkspaceChange),
	}, nil
}

// neverStarted is a failed attempt's result with nothing in it: no output, no
// outcome, no tokens and no agent output. Callers return StageResult{} before the
// agent runs; an unverified completion carries an Output and is not this.
func neverStarted(res stage.StageResult) bool {
	if len(res.Output) > 0 || res.Outcome != "" || res.TokenUsage != 0 {
		return false
	}
	if res.Progress != "" || res.Plan != "" {
		return false
	}
	return len(res.FailedTools) == 0 && len(res.FilesTouched) == 0
}
