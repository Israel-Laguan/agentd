package worker

import (
	"context"
	"fmt"

	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/models"
)

type LoopStatus = agentruntime.LoopStatus
type LoopMeta = agentruntime.LoopMeta
type LoopResult = agentruntime.LoopResult

const (
	LoopSuccessfulCompletion = agentruntime.LoopSuccessfulCompletion
	LoopBudgetExhausted      = agentruntime.LoopBudgetExhausted
	LoopTurnLimitExceeded    = agentruntime.LoopTurnLimitExceeded
	LoopToolFailure          = agentruntime.LoopToolFailure
)

func (w *Worker) buildLoopMeta(
	turnCount, tokenUsage, contextChars, contextBudget int,
	lastError, toolName, budgetKind string,
) LoopMeta {
	return agentruntime.BuildLoopMeta(turnCount, tokenUsage, contextChars, contextBudget, lastError, toolName, budgetKind)
}

func (w *Worker) recordLoopResult(result LoopResult) {
	if w.loopResultRecorder != nil {
		w.loopResultRecorder(result)
	}
}

// handleLoopResult maps a typed loop outcome to existing store side effects.
// Full recovery strategies (summarize-and-continue, decomposition) are Tasks 23/27/28.
func (w *Worker) handleLoopResult(ctx context.Context, task models.Task, result LoopResult) {
	switch result.Status {
	case LoopSuccessfulCompletion:
		// commitTextWithProfile already ran inside the loop.
	case LoopBudgetExhausted:
		payload := result.Meta.LastError
		if payload == "" {
			payload = "context or token budget exhausted"
		}
		w.emit(ctx, task, "LOOP_BUDGET_EXHAUSTED", payload)
		w.handleAgentFailure(ctx, task, payload)
	case LoopTurnLimitExceeded:
		w.handleIterationExceeded(ctx, task)
	case LoopToolFailure:
		payload := result.Meta.LastError
		if result.Meta.ToolName != "" {
			payload = fmt.Sprintf("tool %s failed: %s", result.Meta.ToolName, payload)
		}
		if payload == "" {
			payload = "required tool failed repeatedly"
		}
		w.handleAgentFailure(ctx, task, payload)
	}
}
