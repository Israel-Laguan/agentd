package truncation

import "agentd/internal/gateway/spec"

const truncationMarker = "\n...[TRUNCATED BY AGENTD]...\n"

// TruncationMarker is the delimiter inserted between head and tail segments.
const TruncationMarker = truncationMarker

// MiddleOutStrategy removes the middle of oversized content.
type MiddleOutStrategy struct{}

// Name returns the strategy identifier.
func (s MiddleOutStrategy) Name() string {
	return TruncationStrategyMiddleOut
}

// Truncate applies middle-out cutting.
func (s MiddleOutStrategy) Truncate(input string, maxChars int) string {
	if maxChars <= 0 || len(input) <= maxChars {
		return input
	}
	if maxChars <= len(truncationMarker) {
		return input[:maxChars]
	}
	remaining := maxChars - len(truncationMarker)
	head := remaining / 2
	tail := remaining - head
	return input[:head] + truncationMarker + input[len(input)-tail:]
}

// MiddleOut keeps the beginning and end of long content while removing the
// noisy middle, which is usually the least useful part of large logs.
func MiddleOut(input string, maxChars int) string {
	return MiddleOutStrategy{}.Truncate(input, maxChars)
}

func truncateMessages(messages []spec.PromptMessage, maxChars int, strategy TruncationStrategy) []spec.PromptMessage {
	if strategy == nil {
		strategy = MiddleOutStrategy{}
	}
	out := make([]spec.PromptMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		out[i].Content = strategy.Truncate(msg.Content, maxChars)
	}
	return out
}
