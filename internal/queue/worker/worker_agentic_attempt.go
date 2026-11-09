package worker

import (
	"context"
	"errors"
	"fmt"

	"agentd/internal/models"
	"agentd/internal/queue/worker/agentic"
	"agentd/internal/stage"
)

// runAgenticAttempt checks the provider, mounts the task's hooks, runs session
// start, asks the pre-task clarification, then runs the engine on the clarified
// task. A run that any of these stops never reaches the engine.
func (w *Worker) runAgenticAttempt(
	ctx context.Context, engine *agentic.Engine, task models.Task, project models.Project, profile models.AgentProfile,
) (LoopResult, bool) {
	// The attempt stays cancellable through every step, as it was inside the engine.
	ctx, cancel, done := w.registerRunCancel(ctx, task.ID)
	defer done()

	// A provider that cannot call tools fails before any model call, so
	// clarification never asks a human about a doomed run.
	if refusal, refused := agentic.CheckChatTools(w.gateway, task, profile.Provider); refused {
		w.applyAgenticResult(ctx, project, refusal)
		return LoopResult{}, false
	}
	taskHooks, taskCaps := w.MountAgenticHooks(project, profile)
	// Session start validates credentials, so it fails a task before clarification
	// can ask a human anything.
	if err := w.RunSessionStart(ctx, task, project, taskHooks); err != nil {
		w.applyAgenticResult(ctx, project, agentic.Result{Kind: agentic.ResultFailed, Task: task, Err: err})
		return LoopResult{}, false
	}
	clarified, blocked, err := w.RunPreTaskElicitation(ctx, task, project)
	if err != nil {
		w.applyAgenticResult(ctx, project, agentic.Result{Kind: agentic.ResultFailed, Task: clarified, Err: err})
		return LoopResult{}, false
	}
	if blocked {
		return LoopResult{}, false
	}
	result, outcome, ok := engine.Process(ctx, clarified, project, profile, taskHooks, taskCaps)
	// Save before applying the result, which may make the task READY for a
	// concurrent retry. Success clears the note after the coordinator commits.
	detail := result.Meta.LastError
	if outcome.Err != nil {
		detail = outcome.Err.Error() + "\n" + detail
	}
	if failure := w.attempts.currentFailure(task.ID); failure != nil {
		detail = fmt.Sprintf("First failed command: %s\nFailure: %s\n%s", failure.command, failure.failure, detail)
	}
	if detail == "" {
		detail = "Attempt awaiting coordinator checks; see check feedback if it was rejected."
	}
	// An attempt that could not read the last note never ran; keep that note.
	// A stop at a human is not a failure: the board is already set (ResultNone)
	// or the prompt handoff goes to a human.
	stoppedAtHuman := (!ok && outcome.Kind == agentic.ResultNone) || outcome.Kind == agentic.ResultPromptHandoff
	if !errors.Is(outcome.Err, errAttemptHandoffUnavailable) && !stoppedAtHuman {
		if err := w.writeAttemptHandoff(ctx, clarified, stage.StageResult{Progress: outcome.Text}, errors.New(detail)); err != nil {
			liveCtx, cancelLive := liveContext(ctx)
			defer cancelLive()
			w.failTerminal(liveCtx, clarified, fmt.Errorf("persist attempt handoff: %w", err), models.TaskStateFailedRequiresHuman)
			return LoopResult{}, false
		}
	}
	// Process registers its own cancel for the run and deregisters it on
	// return, replacing the one above. Re-register ours so a cancel during the
	// terminal board write still reaches this attempt.
	w.RegisterCancel(task.ID, cancel)
	committed := w.applyAgenticResult(ctx, project, outcome)
	w.DeregisterCancel(task.ID)
	// A coordinator rejection already failed or requeued the task, so the attempt
	// is not a success and the loop result needs no further handling.
	return result, ok && committed
}

// registerRunCancel makes a task run cancellable by its task ID until done is
// called.
func (w *Worker) registerRunCancel(ctx context.Context, taskID string) (context.Context, context.CancelFunc, func()) {
	cancelCtx, cancel := context.WithCancel(ctx)
	w.RegisterCancel(taskID, cancel)
	return cancelCtx, cancel, func() {
		cancel()
		w.DeregisterCancel(taskID)
	}
}

// applyAgenticResult writes the terminal result the engine settled on to the
// board. The engine only decides the outcome (T-041: only the core writes the
// board). It reports false only when a completed result was rejected on commit.
func (w *Worker) applyAgenticResult(ctx context.Context, project models.Project, r agentic.Result) bool {
	switch r.Kind {
	case agentic.ResultFailed:
		w.FailHard(ctx, r.Task, r.Err)
	case agentic.ResultGatewayError:
		w.HandleGatewayError(ctx, r.Task, r.Err)
	case agentic.ResultCompleted:
		return w.commitExecution(ctx, r.Task, project, r.Profile, r.Text)
	case agentic.ResultPromptHandoff:
		w.HandlePromptHandoff(ctx, r.Task, r.Payload)
	}
	return true
}

// commitExecution applies coordinator checks to a completed execution before
// handing its result to review or persisting it.
func (w *Worker) commitExecution(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, text string) bool {
	if w.interceptTieredNeedsContext(ctx, task, text) {
		return true
	}
	if !w.checkGoldenCriteria(ctx, task) {
		return false
	}
	ctx = context.WithValue(ctx, goldenCheckedKey{}, task.ID)
	if err := w.VerifyPostConditions(task, project); err != nil {
		w.failTerminal(ctx, task, err, models.TaskStateFailedRequiresHuman)
		return false
	}
	if err := w.VerifyBreakdownComplete(ctx, task); err != nil {
		if errors.Is(err, models.ErrBreakdownIncomplete) {
			w.failTerminal(ctx, task, err, models.TaskStateFailedRequiresHuman)
		} else {
			w.handleAgentFailure(ctx, task, err.Error())
		}
		return false
	}
	return w.CommitTextWithProfile(ctx, task, text, &profile)
}
