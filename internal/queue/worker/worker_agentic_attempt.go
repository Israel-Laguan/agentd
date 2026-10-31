package worker

import (
	"context"

	"agentd/internal/models"
	"agentd/internal/queue/worker/agentic"
)

// runAgenticAttempt checks the provider, mounts the task's hooks, runs session
// start, asks the pre-task clarification, then runs the engine on the clarified
// task. A run that any of these stops never reaches the engine.
func (w *Worker) runAgenticAttempt(
	ctx context.Context, engine *agentic.Engine, task models.Task, project models.Project, profile models.AgentProfile,
) (LoopResult, bool) {
	// The attempt stays cancellable through every step, as it was inside the engine.
	ctx, done := w.registerRunCancel(ctx, task.ID)
	defer done()

	// A provider that cannot call tools fails before any model call, so
	// clarification never asks a human about a doomed run.
	if refusal, refused := agentic.CheckChatTools(w.gateway, task, profile.Provider); refused {
		w.applyAgenticResult(ctx, refusal)
		return LoopResult{}, false
	}
	taskHooks, taskCaps := w.MountAgenticHooks(project, profile)
	// Session start validates credentials, so it fails a task before clarification
	// can ask a human anything.
	if err := w.RunSessionStart(ctx, task, project, taskHooks); err != nil {
		w.applyAgenticResult(ctx, agentic.Result{Kind: agentic.ResultFailed, Task: task, Err: err})
		return LoopResult{}, false
	}
	clarified, blocked, err := w.RunPreTaskElicitation(ctx, task, project)
	if err != nil {
		w.applyAgenticResult(ctx, agentic.Result{Kind: agentic.ResultFailed, Task: clarified, Err: err})
		return LoopResult{}, false
	}
	if blocked {
		return LoopResult{}, false
	}
	result, outcome, ok := engine.Process(ctx, clarified, project, profile, taskHooks, taskCaps)
	w.applyAgenticResult(ctx, outcome)
	return result, ok
}

// registerRunCancel makes a task run cancellable by its task ID until done is
// called.
func (w *Worker) registerRunCancel(ctx context.Context, taskID string) (context.Context, func()) {
	cancelCtx, cancel := context.WithCancel(ctx)
	w.RegisterCancel(taskID, cancel)
	return cancelCtx, func() {
		cancel()
		w.DeregisterCancel(taskID)
	}
}

// applyAgenticResult writes the terminal result the engine settled on to the
// board. The engine only decides the outcome (T-041: only the core writes the
// board).
func (w *Worker) applyAgenticResult(ctx context.Context, r agentic.Result) {
	switch r.Kind {
	case agentic.ResultFailed:
		w.FailHard(ctx, r.Task, r.Err)
	case agentic.ResultGatewayError:
		w.HandleGatewayError(ctx, r.Task, r.Err)
	case agentic.ResultCompleted:
		w.CommitTextWithProfile(ctx, r.Task, r.Text, &r.Profile)
	case agentic.ResultPromptHandoff:
		w.HandlePromptHandoff(ctx, r.Task, r.Payload)
	}
}
