package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func encodeCapabilityResult(out any) (string, error) {
	if s, ok := out.(string); ok {
		return s, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("capability result encode: %w", err)
	}
	return string(encoded), nil
}

func (w *Worker) commitCapabilityRouteResult(
	ctx context.Context,
	task models.Task,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
	text string,
) LoopResult {
	if w.messageEditor != nil {
		w.messageEditor.Commit(messages, gateway.PromptMessage{
			Role:    "assistant",
			Content: text,
		})
	} else if messages != nil {
		*messages = append(*messages, gateway.PromptMessage{
			Role:    "assistant",
			Content: text,
		})
	}
	w.commitTextWithProfile(ctx, task, text, &profile)
	return LoopResult{Status: LoopSuccessfulCompletion}
}

// tryExternalCapabilityRoute classifies the task, dispatches to an external adapter when
// configured, commits session history, and returns without entering the agentic turn loop.
func (w *Worker) tryExternalCapabilityRoute(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
) (LoopResult, bool) {
	_ = project
	if w.capabilityRouter == nil {
		return LoopResult{}, false
	}

	decision, ok := w.capabilityRouter.Route(task, profile)
	if !ok {
		return LoopResult{}, false
	}

	if w.capabilities == nil {
		slog.Warn("capability routing: no capability registry; falling through to agentic loop",
			"task_id", task.ID,
			"intent", decision.Intent,
			"adapter", decision.Adapter,
		)
		return LoopResult{}, false
	}

	adapter, found := w.capabilities.GetAdapter(decision.Adapter)
	if !found || adapter == nil {
		slog.Warn("capability routing: adapter not registered; falling through to agentic loop",
			"task_id", task.ID,
			"intent", decision.Intent,
			"adapter", decision.Adapter,
		)
		return LoopResult{}, false
	}

	out, err := w.capabilities.CallTool(ctx, decision.Adapter, decision.Tool, decision.Args)
	if err != nil {
		w.failHard(ctx, task, fmt.Errorf("capability routing: %s/%s: %w", decision.Adapter, decision.Tool, err))
		return LoopResult{}, false
	}

	text, err := encodeCapabilityResult(out)
	if err != nil {
		w.failHard(ctx, task, err)
		return LoopResult{}, false
	}

	return w.commitCapabilityRouteResult(ctx, task, profile, messages, text), true
}
