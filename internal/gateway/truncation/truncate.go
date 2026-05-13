package truncation

import (
	"fmt"
	"unicode/utf8"

	"agentd/internal/gateway/spec"
)

const truncationMarker = "【...】"

// TruncationMarker is inserted when a message or section is truncated.
const TruncationMarker = truncationMarker

// CollapseMarkerFor returns a marker indicating that N tool exchanges have been collapsed.
func CollapseMarkerFor(n int) string {
	return fmt.Sprintf("【%d tool exchanges collapsed】", n)
}

// CollapseMarker is the default marker (for backwards compatibility)
const CollapseMarker = "【N tool exchanges collapsed】"

// MiddleOutStrategy removes the middle of oversized content.
type MiddleOutStrategy struct{}

// Name returns the strategy identifier.
func (s MiddleOutStrategy) Name() string {
	return TruncationStrategyMiddleOut
}

// Truncate applies middle-out cutting with UTF-8 safe slicing.
func (s MiddleOutStrategy) Truncate(input string, maxChars int) string {
	if maxChars <= 0 || len(input) <= maxChars {
		return input
	}
	markerBytes := len(truncationMarker)
	if maxChars <= markerBytes {
		return utf8SafePrefix(input, maxChars)
	}
	remaining := maxChars - markerBytes
	headBytes := remaining / 2
	tailBytes := remaining - headBytes

	return utf8SafePrefix(input, headBytes) + truncationMarker + utf8SafeSuffix(input, tailBytes)
}

// MiddleOut keeps the beginning and end of long content while removing the
// noisy middle, which is usually the least useful part of large logs.
func MiddleOut(input string, maxChars int) string {
	return MiddleOutStrategy{}.Truncate(input, maxChars)
}

func utf8SafePrefix(input string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if maxBytes >= len(input) {
		return input
	}
	for maxBytes > 0 && !utf8.ValidString(input[:maxBytes]) {
		maxBytes--
	}
	return input[:maxBytes]
}

func utf8SafeSuffix(input string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if maxBytes >= len(input) {
		return input
	}
	start := len(input) - maxBytes
	for start < len(input) && !utf8.RuneStart(input[start]) {
		start++
	}
	return input[start:]
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
