package truncation

import (
	"agentd/internal/gateway/spec"
)

// applyBudgetTruncation handles character budget truncation.
// The loop shrinks the message list until it fits the budget. A convergence guard
// breaks the loop when a pass no longer reduces the character count — this happens
// when the budget is below the irreducible truncation-marker overhead, where the
// best-effort result (a single marker) still exceeds the budget. Without this guard
// the loop would spin forever.
func (t *AgenticTruncator) applyBudgetTruncation(messages []spec.PromptMessage, budget int) []spec.PromptMessage {
	for budget > 0 && totalChars(messages) > budget {
		prev := totalChars(messages)
		messages = t.truncateToBudget(messages, budget)
		messages = t.removeDanglingToolCalls(messages)
		if totalChars(messages) >= prev {
			break // no progress: budget below irreducible marker overhead
		}
	}
	return messages
}

// applyMessageCountTruncation handles message count truncation with tool exchange awareness
func (t *AgenticTruncator) applyMessageCountTruncation(messages []spec.PromptMessage) []spec.PromptMessage {
	exchanges := findToolExchanges(messages)
	out := make([]spec.PromptMessage, 0, t.MaxMessages)
	out = append(out, messages[0]) // Keep first system prompt

	firstUserIdx := t.findFirstUserIndex(messages)
	// Preserve all leading system messages before the first user so
	// memory-lesson anchors are not dropped by message-count truncation.
	for i := 1; firstUserIdx > 0 && i < firstUserIdx; i++ {
		if messages[i].Role == "system" && len(out) < t.MaxMessages {
			out = append(out, messages[i])
		}
	}

	// Only append first user anchor if we haven't exceeded MaxMessages
	if firstUserIdx > 0 && len(out) < t.MaxMessages {
		out = append(out, messages[firstUserIdx])
	}

	remaining := t.MaxMessages - len(out)
	if remaining > 0 {
		return t.truncateToMessageLimit(messages, out, exchanges, remaining, firstUserIdx)
	}

	return out
}

// truncateToMessageLimit handles the complex logic of truncating to message limit
func (t *AgenticTruncator) truncateToMessageLimit(messages []spec.PromptMessage, out []spec.PromptMessage, exchanges []toolExchange, remaining int, firstUserIdx int) []spec.PromptMessage {
	startFrom := len(messages) - remaining
	minIdx := 1
	if firstUserIdx > 0 {
		minIdx = firstUserIdx + 1
	}
	if startFrom < minIdx {
		startFrom = minIdx
	}

	if startFrom >= len(messages) {
		return out
	}

	truncated := startFrom > minIdx
	originalStartFrom := startFrom

	// Skip tool messages at the boundary
	for startFrom < len(messages) && messages[startFrom].Role == "tool" {
		startFrom++
		truncated = true
	}

	// Count dropped tool exchanges
	droppedExchanges := t.countDroppedExchanges(exchanges, minIdx, originalStartFrom)

	if startFrom < len(messages) {
		if truncated {
			msg := messages[startFrom]
			msg = t.addTruncationMarkers(msg, truncated, droppedExchanges)
			out = append(out, msg)
			startFrom++
		} else if droppedExchanges > 0 {
			msg := messages[startFrom]
			msg.Content = CollapseMarkerFor(droppedExchanges) + " " + msg.Content
			out = append(out, msg)
			startFrom++
		}
	}

	if startFrom < len(messages) {
		out = append(out, messages[startFrom:]...)
	}

	return out
}

// countDroppedExchanges counts how many tool exchanges were completely dropped
func (t *AgenticTruncator) countDroppedExchanges(exchanges []toolExchange, minIdx, originalStartFrom int) int {
	droppedExchanges := 0
	for _, ex := range exchanges {
		if ex.assistantIndex >= minIdx && ex.assistantIndex < originalStartFrom {
			droppedExchanges++
		}
	}
	return droppedExchanges
}
