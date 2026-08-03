package truncation

import (
	"context"
	"strings"

	"agentd/internal/gateway/spec"
)

// Apply performs agentic truncation on the message list
func (t *AgenticTruncator) Apply(_ context.Context, messages []spec.PromptMessage, budget int) ([]spec.PromptMessage, error) {
	// Early return if within limits
	if t.withinLimits(messages, budget) {
		return messages, nil
	}

	// Apply budget truncation if needed
	messages = t.applyBudgetTruncation(messages, budget)

	// Apply message count truncation if still needed
	if len(messages) > t.MaxMessages {
		messages = t.applyMessageCountTruncation(messages)
	}

	// Final cleanup
	return t.finalizeTruncation(messages, budget), nil
}

// withinLimits checks if messages are within both message count and budget limits
func (t *AgenticTruncator) withinLimits(messages []spec.PromptMessage, budget int) bool {
	return len(messages) <= t.MaxMessages && (budget <= 0 || totalChars(messages) <= budget)
}

// finalizeTruncation performs final cleanup and budget enforcement.
// It first strips dangling tool calls, then delegates to the shared
// truncateWithBudgetLoop convergence guard. Without the guard the loop
// would spin forever on budgets below the irreducible truncation-marker
// overhead (best effort is a single marker).
func (t *AgenticTruncator) finalizeTruncation(messages []spec.PromptMessage, budget int) []spec.PromptMessage {
	out := t.removeDanglingToolCalls(messages)
	return t.truncateWithBudgetLoop(out, budget)
}

// findFirstUserIndex finds the index of the first user message.
// Searches from index 0 so that a leading user message (not preceded by a
// system prompt) is correctly identified as the task anchor.
func (t *AgenticTruncator) findFirstUserIndex(messages []spec.PromptMessage) int {
	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "user" {
			return i
		}
	}
	return -1
}

// addTruncationMarkers adds truncation and collapse markers to a message
func (t *AgenticTruncator) addTruncationMarkers(msg spec.PromptMessage, truncated bool, droppedExchanges int) spec.PromptMessage {
	if truncated && !strings.Contains(msg.Content, TruncationMarker) && !strings.Contains(msg.Content, "tool exchanges collapsed") {
		msg.Content = TruncationMarker + msg.Content
	}

	if droppedExchanges > 0 && !strings.Contains(msg.Content, "tool exchanges collapsed") {
		msg.Content = CollapseMarkerFor(droppedExchanges) + " " + msg.Content
	}

	return msg
}
