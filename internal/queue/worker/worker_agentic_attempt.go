package worker

import (
	"context"

	"agentd/internal/models"

	"agentd/internal/queue/worker/agentic"
)

// runAgenticAttempt asks the pre-task clarification, then runs the engine on the
// clarified task. A run the clarification blocks or fails never reaches the
// engine.
func (w *Worker) runAgenticAttempt(
	ctx context.Context, engine *agentic.Engine, task models.Task, project models.Project, profile models.AgentProfile,
) (LoopResult, bool) {
	// A provider that cannot call tools fails before any model call, as the
	// engine would, so elicitation never asks a human about a doomed run.
	if refusal, refused := agentic.CheckChatTools(w.gateway, task, profile.Provider); refused {
		w.applyAgenticResult(ctx, refusal)
		return LoopResult{}, false
	}
	clarified, blocked, err := w.runAgenticElicitation(ctx, task, project)
	if err != nil {
		w.applyAgenticResult(ctx, agentic.Result{Kind: agentic.ResultFailed, Task: clarified, Err: err})
		return LoopResult{}, false
	}
	if blocked {
		return LoopResult{}, false
	}
	result, outcome, ok := engine.Process(ctx, clarified, project, profile)
	w.applyAgenticResult(ctx, outcome)
	return result, ok
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
