package truncation

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"agentd/internal/gateway/spec"
)

const summarizeTimeout = 30 * time.Second

// SummarizeTruncator summarizes oversized messages via the LLM when possible.
type SummarizeTruncator struct {
	Gateway  spec.AIGateway
	Breaker  spec.BreakerChecker
	Fallback TruncationStrategy
}

// Apply implements spec.Truncator.
func (t SummarizeTruncator) Apply(ctx context.Context, messages []spec.PromptMessage, budget int) ([]spec.PromptMessage, error) {
	out := make([]spec.PromptMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		if budget <= 0 || utf8.RuneCountInString(msg.Content) <= budget {
			continue
		}
		out[i].Content = t.summarizeOrFallback(ctx, msg.Content, budget)
	}
	return out, nil
}

func (t SummarizeTruncator) summarizeOrFallback(ctx context.Context, content string, budget int) string {
	fallback := t.Fallback
	if fallback == nil {
		fallback = MiddleOutStrategy{}
	}
	if t.Gateway == nil || (t.Breaker != nil && t.Breaker.IsOpen()) {
		return fallback.Truncate(content, budget)
	}
	summaryCtx, cancel := context.WithTimeout(ctx, summarizeTimeout)
	defer cancel()
	resp, err := t.Gateway.Generate(summaryCtx, spec.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: "Summarize the following content concisely, preserving key facts, decisions, and action items. Output only the summary."},
			{Role: "user", Content: content},
		},
		Temperature:    0.1,
		SkipTruncation: true,
	})
	if err != nil {
		return fallback.Truncate(content, budget)
	}
	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		return fallback.Truncate(content, budget)
	}
	if utf8.RuneCountInString(summary) > budget {
		return fallback.Truncate(summary, budget)
	}
	return summary
}
