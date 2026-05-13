package truncation

import (
	"context"
	"strings"
	"unicode/utf8"

	"agentd/internal/gateway/spec"
)

type AgenticTruncator struct {
	MaxMessages int
}

// toolExchange represents a pair of assistant tool_calls and corresponding tool response
type toolExchange struct {
	assistantIndex int
	toolIndices    []int
}

// findToolExchanges identifies assistant messages with tool_calls and their corresponding tool responses
func findToolExchanges(messages []spec.PromptMessage) []toolExchange {
	var exchanges []toolExchange

	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			exchange := toolExchange{
				assistantIndex: i,
				toolIndices:    []int{},
			}

			// Find all tool responses matching any of the tool call IDs
			for _, tc := range messages[i].ToolCalls {
				for j := i + 1; j < len(messages); j++ {
					if messages[j].Role == "tool" && messages[j].ToolCallID == tc.ID {
						exchange.toolIndices = append(exchange.toolIndices, j)
					}
				}
			}

			exchanges = append(exchanges, exchange)
		}
	}

	return exchanges
}

func NewAgenticTruncator(maxMessages int) spec.Truncator {
	if maxMessages <= 0 {
		maxMessages = 20
	}
	return &AgenticTruncator{MaxMessages: maxMessages}
}

func (t *AgenticTruncator) Apply(_ context.Context, messages []spec.PromptMessage, budget int) ([]spec.PromptMessage, error) {
	// Check if we're within both limits - only return early if both are satisfied
	if len(messages) <= t.MaxMessages && (budget <= 0 || totalChars(messages) <= budget) {
		return messages, nil
	}

	// If budget is specified and exceeded, apply character budget truncation first
	if budget > 0 && totalChars(messages) > budget {
		messages = t.truncateToBudget(messages, budget)
		// After character budget truncation, check if message count is now under limit
		if len(messages) <= t.MaxMessages {
			messages = t.removeDanglingToolCalls(messages)
			// Double-check character budget after pairwise cleanup
			if budget <= 0 || totalChars(messages) <= budget {
				return messages, nil
			}
			// Still over budget - need to truncate more
		}
	}

	// If message count is now under limit but budget is still exceeded,
	// we need to apply budget truncation (this happens when MaxMessages was already high)
	if budget > 0 && totalChars(messages) > budget && len(messages) <= t.MaxMessages {
		messages = t.truncateToBudget(messages, budget)
		messages = t.removeDanglingToolCalls(messages)
		// Final hard-budget enforcement: ensure output never exceeds budget
		for budget > 0 && totalChars(messages) > budget {
			messages = t.truncateToBudget(messages, budget)
		}
		return messages, nil
	}

	// Apply message count limit truncation
	// Find all tool exchanges before truncation
	exchanges := findToolExchanges(messages)

	out := make([]spec.PromptMessage, 0, t.MaxMessages)
	out = append(out, messages[0]) // Keep system prompt

	firstUserIdx := -1
	for i := 1; i < len(messages); i++ {
		if messages[i].Role == "user" {
			firstUserIdx = i
			break
		}
	}

	if firstUserIdx > 0 {
		out = append(out, messages[firstUserIdx])
	}

	remaining := t.MaxMessages - len(out)
	if remaining > 0 {
		startFrom := len(messages) - remaining
		minIdx := 1
		if firstUserIdx > 0 {
			minIdx = firstUserIdx + 1
		}
		if startFrom < minIdx {
			startFrom = minIdx
		}

		if startFrom < len(messages) {
			truncated := startFrom > minIdx

			// Skip tool messages at the boundary
			originalStartFrom := startFrom
			for startFrom < len(messages) && messages[startFrom].Role == "tool" {
				startFrom++
				truncated = true
			}

			// Count how many tool exchanges were completely dropped
			droppedExchanges := 0
			for _, ex := range exchanges {
				if ex.assistantIndex >= minIdx && ex.assistantIndex < originalStartFrom {
					droppedExchanges++
				}
			}

			if startFrom < len(messages) && truncated {
				msg := messages[startFrom]
				// Add truncation marker only if not already from budget truncation
				if !containsString(msg.Content, TruncationMarker) && !containsString(msg.Content, "tool exchanges collapsed") {
					msg.Content = TruncationMarker + msg.Content
				}

				// If we dropped tool exchanges, add collapse marker to this message
				if droppedExchanges > 0 && !containsString(msg.Content, "tool exchanges collapsed") {
					msg.Content = CollapseMarkerFor(droppedExchanges) + " " + msg.Content
				}

				out = append(out, msg)
				startFrom++
			} else if droppedExchanges > 0 && startFrom < len(messages) {
				// Add collapse marker to the first retained message
				msg := messages[startFrom]
				if !containsString(msg.Content, "tool exchanges collapsed") {
					msg.Content = CollapseMarkerFor(droppedExchanges) + " " + msg.Content
				}
				out = append(out, msg)
				startFrom++
			}

			if startFrom < len(messages) {
				out = append(out, messages[startFrom:]...)
			}
		}
	}

	// Remove dangling assistant tool_calls without corresponding tool responses
	out = t.removeDanglingToolCalls(out)

	return out, nil
}

// containsString is a helper to check if a string contains a substring
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// removeDanglingToolCalls ensures pairwise consistency:
// - If an assistant message has ToolCalls, ensure corresponding tool responses exist
// - If not, mark the ToolCalls as collapsed instead of leaving orphans
func (t *AgenticTruncator) removeDanglingToolCalls(messages []spec.PromptMessage) []spec.PromptMessage {
	if len(messages) == 0 {
		return messages
	}

	out := make([]spec.PromptMessage, len(messages))
	copy(out, messages)

	// Check each assistant message with tool_calls
	for i := 0; i < len(out); i++ {
		if out[i].Role == "assistant" && len(out[i].ToolCalls) > 0 {
			// Check if all tool calls have corresponding tool responses
			hasOrphanToolCalls := false
			for _, tc := range out[i].ToolCalls {
				found := false
				for j := i + 1; j < len(out); j++ {
					if out[j].Role == "tool" && out[j].ToolCallID == tc.ID {
						found = true
						break
					}
				}
				if !found {
					hasOrphanToolCalls = true
					break
				}
			}

			if hasOrphanToolCalls {
				// Add collapse marker to the content and clear tool calls
				if out[i].Content != "" {
					out[i].Content = out[i].Content + " " + CollapseMarker
				} else {
					out[i].Content = CollapseMarker
				}
				out[i].ToolCalls = nil
			}
		}
	}

	return out
}

// totalChars calculates the total character count of all message contents including tool calls
func totalChars(messages []spec.PromptMessage) int {
	total := 0
	for _, m := range messages {
		total += utf8.RuneCountInString(m.Content)
		// Also count tool call function names and arguments
		for _, tc := range m.ToolCalls {
			total += utf8.RuneCountInString(tc.Function.Name)
			total += utf8.RuneCountInString(tc.Function.Arguments)
		}
	}
	return total
}

// truncateToBudget reduces message content to fit within character budget.
// It preserves anchors (system prompt, first user) and removes from the middle.
func (t *AgenticTruncator) truncateToBudget(messages []spec.PromptMessage, budget int) []spec.PromptMessage {
	if len(messages) == 0 || budget <= 0 {
		return messages
	}

	// Reserve space for markers that will be added during truncation
	// Max overhead: TruncationMarker + space + CollapseMarker + space
	markerOverhead := utf8.RuneCountInString(TruncationMarker) + utf8.RuneCountInString(CollapseMarker) + 2
	effectiveBudget := budget - markerOverhead
	// Clamp: don't let the floor exceed the caller budget
	if effectiveBudget < 0 {
		effectiveBudget = 0
	}

	// Always keep system prompt (first message)
	out := []spec.PromptMessage{messages[0]}

	// Find first user message (anchor)
	firstUserIdx := -1
	for i := 1; i < len(messages); i++ {
		if messages[i].Role == "user" {
			firstUserIdx = i
			break
		}
	}

	// Add first user message if found
	if firstUserIdx > 0 {
		out = append(out, messages[firstUserIdx])
	}

	// Calculate budget remaining after anchors
	anchorChars := 0
	for _, m := range out {
		anchorChars += utf8.RuneCountInString(m.Content)
	}
	remainingBudget := effectiveBudget - anchorChars

	if remainingBudget <= 0 {
		// Anchors alone exceed budget - truncate them
		return t.truncateAnchorsToBudget(out, effectiveBudget)
	}

	// Collect non-anchor messages (middle content)
	middle := []spec.PromptMessage{}
	if firstUserIdx > 0 {
		middle = messages[firstUserIdx+1:]
	} else if len(messages) > 1 {
		middle = messages[1:]
	}

	// If middle fits in remaining budget, include all
	if totalChars(middle) <= remainingBudget {
		out = append(out, middle...)
		return out
	}

	// Need to truncate middle content - take from the end (most recent)
	out = append(out, t.truncateMiddleToBudget(middle, remainingBudget)...)

	return out
}

// truncateAnchorsToBudget truncates anchor messages themselves when they exceed budget
func (t *AgenticTruncator) truncateAnchorsToBudget(messages []spec.PromptMessage, budget int) []spec.PromptMessage {
	out := make([]spec.PromptMessage, 0, len(messages))
	remaining := budget

	for _, m := range messages {
		if remaining <= 0 {
			break
		}
		msg := m
		// Reserve space for marker before slicing
		markerLen := utf8.RuneCountInString(TruncationMarker)
		if utf8.RuneCountInString(msg.Content) > remaining {
			// Reserve space for marker
			keep := remaining - markerLen
			if keep < 0 {
				keep = 0
			}
			// Use rune-based slicing
			runes := []rune(msg.Content)
			msg.Content = string(runes[:keep]) + TruncationMarker
			remaining -= utf8.RuneCountInString(msg.Content)
		} else {
			remaining -= utf8.RuneCountInString(msg.Content)
		}
		out = append(out, msg)
	}

	return out
}

// truncateMiddleToBudget truncates middle messages from the most recent
func (t *AgenticTruncator) truncateMiddleToBudget(messages []spec.PromptMessage, budget int) []spec.PromptMessage {
	if len(messages) == 0 || budget <= 0 {
		return messages
	}

	// Collect messages in reverse order (most recent first) to avoid O(N^2) prepend
	var reversed []spec.PromptMessage
	truncated := false
	markerLen := utf8.RuneCountInString(TruncationMarker)

	// Iterate from most recent (end) to oldest (start)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		msgLen := utf8.RuneCountInString(msg.Content)

		if budget >= msgLen {
			// Add this message (in reverse order)
			reversed = append(reversed, msg)
			budget -= msgLen
		} else if budget > 0 {
			// Truncate this message to fit - reserve space for marker
			keep := budget - markerLen
			if keep < 0 {
				keep = 0
			}
			// Use rune-based slicing
			runes := []rune(msg.Content)
			truncatedContent := string(runes[:keep]) + TruncationMarker
			truncatedMsg := spec.PromptMessage{
				Role:       msg.Role,
				Content:    truncatedContent,
				ToolCalls:  msg.ToolCalls,
				ToolCallID: msg.ToolCallID,
			}
			reversed = append(reversed, truncatedMsg)
			truncated = true
			budget = 0
		} else {
			truncated = true
		}
	}

	// Reverse to get correct order (oldest to most recent)
	out := make([]spec.PromptMessage, len(reversed))
	for i, msg := range reversed {
		out[len(reversed)-1-i] = msg
	}

	// If we had to drop any messages, add collapse marker
	if truncated && len(out) > 0 {
		droppedUntil := len(messages) - len(out)
		droppedExchanges := 0
		for _, ex := range findToolExchanges(messages) {
			if ex.assistantIndex < droppedUntil {
				droppedExchanges++
			}
		}
		if droppedExchanges > 0 {
			// Add collapse marker to first message
			out[0].Content = CollapseMarkerFor(droppedExchanges) + " " + out[0].Content
		}
	}

	return out
}
