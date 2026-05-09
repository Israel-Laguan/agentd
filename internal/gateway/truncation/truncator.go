package truncation

import (
	"context"
	"fmt"

	"agentd/internal/gateway/spec"
)

const (
	TruncatorPolicyMiddleOut = "middle_out"
	TruncatorPolicyHeadTail  = "head_tail"
	TruncatorPolicySummarize = "summarize"
	TruncatorPolicyReject    = "reject"
)

// StrategyTruncator applies a TruncationStrategy per message.
type StrategyTruncator struct {
	Strategy TruncationStrategy
}

// NewTruncator builds a truncator from policy name and optional summarize gateway.
func NewTruncator(policy string, headRatio float64, gateway spec.AIGateway, breaker spec.BreakerChecker) spec.Truncator {
	switch policy {
	case TruncatorPolicyHeadTail:
		return StrategyTruncator{Strategy: HeadTailStrategy{HeadRatio: headRatio}}
	case TruncatorPolicySummarize:
		return SummarizeTruncator{Gateway: gateway, Breaker: breaker, Fallback: MiddleOutStrategy{}}
	case TruncatorPolicyReject:
		return RejectTruncator{}
	case TruncatorPolicyMiddleOut, "":
		return StrategyTruncator{Strategy: MiddleOutStrategy{}}
	default:
		return StrategyTruncator{Strategy: MiddleOutStrategy{}}
	}
}

// Apply implements spec.Truncator.
func (t StrategyTruncator) Apply(_ context.Context, messages []spec.PromptMessage, budget int) ([]spec.PromptMessage, error) {
	strategy := t.Strategy
	if strategy == nil {
		strategy = MiddleOutStrategy{}
	}
	out := make([]spec.PromptMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		out[i].Content = strategy.Truncate(msg.Content, budget)
	}
	return out, nil
}

// RejectTruncator rejects when any message exceeds budget.
type RejectTruncator struct{}

// Apply implements spec.Truncator.
func (t RejectTruncator) Apply(_ context.Context, messages []spec.PromptMessage, budget int) ([]spec.PromptMessage, error) {
	if budget <= 0 {
		return append([]spec.PromptMessage(nil), messages...), nil
	}
	for _, msg := range messages {
		if len(msg.Content) > budget {
			return nil, fmt.Errorf("%w: message content length %d exceeds budget %d", spec.ErrContextBudgetExceeded, len(msg.Content), budget)
		}
	}
	return append([]spec.PromptMessage(nil), messages...), nil
}
