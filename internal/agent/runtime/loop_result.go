package runtime

import "fmt"

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

// BuildLoopMeta constructs loop diagnostic metadata.
func BuildLoopMeta(
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
