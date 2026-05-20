package worker

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

// LoopStatus identifies why the agentic inner loop stopped.
type LoopStatus int

const (
	LoopSuccessfulCompletion LoopStatus = iota
	LoopBudgetExhausted
	LoopTurnLimitExceeded
	LoopToolFailure
)

func (s LoopStatus) String() string {
	switch s {
	case LoopSuccessfulCompletion:
		return "successful_completion"
	case LoopBudgetExhausted:
		return "budget_exhausted"
	case LoopTurnLimitExceeded:
		return "turn_limit_exceeded"
	case LoopToolFailure:
		return "tool_failure"
	default:
		return fmt.Sprintf("unknown_loop_status(%d)", int(s))
	}
}

// LoopMeta carries diagnostic metadata for the caller's recovery strategy.
type LoopMeta struct {
	TurnCount     int
	TokenUsage    int
	ContextChars  int
	ContextBudget int
	LastError     string
	ToolName      string
	BudgetKind    string // "token" or "context"
}

// LoopResult is the typed outcome of processAgentic when ok is true.
type LoopResult struct {
	Status LoopStatus
	Meta   LoopMeta
}

// IsTerminalSuccess reports whether the loop completed with a final response.
func (r LoopResult) IsTerminalSuccess() bool {
	return r.Status == LoopSuccessfulCompletion
}

func (w *Worker) buildLoopMeta(
	turnCount, tokenUsage, contextChars, contextBudget int,
	lastError, toolName, budgetKind string,
) LoopMeta {
	return LoopMeta{
		TurnCount:     turnCount,
		TokenUsage:    tokenUsage,
		ContextChars:  contextChars,
		ContextBudget: contextBudget,
		LastError:     lastError,
		ToolName:      toolName,
		BudgetKind:    budgetKind,
	}
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
