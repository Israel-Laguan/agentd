package truncation

import (
	"unicode/utf8"

	"agentd/internal/gateway/spec"
)

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

	markerOverhead := utf8.RuneCountInString(TruncationMarker) + utf8.RuneCountInString(CollapseMarker) + 2
	effectiveBudget := budget - markerOverhead
	if effectiveBudget < 0 {
		effectiveBudget = 0
	}

	anchors := t.buildAnchorsFromMessages(messages)
	anchorChars := t.calcAnchorChars(anchors)
	remainingBudget := effectiveBudget - anchorChars

	if remainingBudget <= 0 {
		return t.truncateAnchorsToBudget(anchors, effectiveBudget)
	}

	middle := t.collectMiddleMessages(messages, anchors)
	if totalChars(middle) <= remainingBudget {
		return append(anchors, middle...)
	}

	return append(anchors, t.truncateMiddleToBudget(middle, remainingBudget)...)
}

// buildAnchorsFromMessages builds anchor messages (system prompt + leading system + first user).
func (t *AgenticTruncator) buildAnchorsFromMessages(messages []spec.PromptMessage) []spec.PromptMessage {
	if len(messages) == 0 {
		return nil
	}
	out := []spec.PromptMessage{messages[0]}

	firstUserIdx := -1
	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "user" {
			firstUserIdx = i
			break
		}
	}

	for i := 1; firstUserIdx > 0 && i < firstUserIdx; i++ {
		if messages[i].Role == "system" {
			out = append(out, messages[i])
		}
	}

	if firstUserIdx > 0 {
		out = append(out, messages[firstUserIdx])
	}
	return out
}

// calcAnchorChars calculates total character count of anchor messages.
func (t *AgenticTruncator) calcAnchorChars(messages []spec.PromptMessage) int {
	total := 0
	for _, m := range messages {
		total += utf8.RuneCountInString(m.Content)
	}
	return total
}

// collectMiddleMessages collects non-anchor messages for truncation.
func (t *AgenticTruncator) collectMiddleMessages(all, anchors []spec.PromptMessage) []spec.PromptMessage {
	if len(all) <= 1 {
		return nil
	}
	firstUserIdx := -1
	for i := 0; i < len(all); i++ {
		if all[i].Role == "user" {
			firstUserIdx = i
			break
		}
	}
	if firstUserIdx > 0 {
		return all[firstUserIdx+1:]
	}
	return all[1:]
}

// truncateAnchorsToBudget truncates anchor messages themselves when they exceed budget.
// Non-user anchors are processed first; the first user anchor is guaranteed to get
// at least the truncation marker so the user's actual task is not silently dropped.
func (t *AgenticTruncator) truncateAnchorsToBudget(messages []spec.PromptMessage, budget int) []spec.PromptMessage {
	out := make([]spec.PromptMessage, 0, len(messages))
	remaining := budget

	var userAnchors []spec.PromptMessage
	var otherAnchors []spec.PromptMessage
	for _, m := range messages {
		if m.Role == "user" {
			userAnchors = append(userAnchors, m)
		} else {
			otherAnchors = append(otherAnchors, m)
		}
	}

	// Process non-user anchors first so the user anchor gets whatever budget remains.
	for _, m := range otherAnchors {
		if remaining <= 0 {
			break
		}
		msg := m
		markerLen := utf8.RuneCountInString(TruncationMarker)
		if utf8.RuneCountInString(msg.Content) > remaining {
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

	// Ensure user anchor gets at least the truncation marker so the user's task
	// is represented even in a zero-budget scenario.
	for _, m := range userAnchors {
		msg := m
		markerLen := utf8.RuneCountInString(TruncationMarker)
		if remaining <= 0 {
			msg.Content = TruncationMarker
		} else if utf8.RuneCountInString(msg.Content) > remaining {
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

	reversed := t.collectMessagesInReverse(messages, &budget)
	out := t.reverseMessages(reversed)

	if len(out) < len(messages) && len(out) > 0 {
		out = t.addCollapseMarkerIfNeeded(out, messages)
	}

	return out
}

// collectMessagesInReverse collects messages from most recent to oldest, applying budget constraints
func (t *AgenticTruncator) collectMessagesInReverse(messages []spec.PromptMessage, budget *int) []spec.PromptMessage {
	var reversed []spec.PromptMessage
	markerLen := utf8.RuneCountInString(TruncationMarker)

	// Iterate from most recent (end) to oldest (start)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		// Calculate message length including content AND tool call arguments
		msgLen := utf8.RuneCountInString(msg.Content)
		for _, tc := range msg.ToolCalls {
			msgLen += utf8.RuneCountInString(tc.Function.Name)
			msgLen += utf8.RuneCountInString(tc.Function.Arguments)
		}

		if *budget >= msgLen {
			reversed = append(reversed, msg)
			*budget -= msgLen
		} else if *budget > 0 {
			truncatedMsg := t.truncateSingleMessage(msg, *budget-markerLen)
			reversed = append(reversed, truncatedMsg)
			*budget = 0
		}
		// If budget <= 0, message is dropped (truncated = true)
	}

	return reversed
}

// truncateSingleMessage truncates a single message to fit within budget
func (t *AgenticTruncator) truncateSingleMessage(msg spec.PromptMessage, keep int) spec.PromptMessage {
	if keep < 0 {
		keep = 0
	}
	runes := []rune(msg.Content)
	// Clamp keep to content length to avoid panic on short/empty content
	if keep > len(runes) {
		keep = len(runes)
	}
	truncatedContent := string(runes[:keep]) + TruncationMarker

	return spec.PromptMessage{
		Role:       msg.Role,
		Content:    truncatedContent,
		ToolCalls:  nil, // Clear tool calls to ensure budget enforcement terminates
		ToolCallID: msg.ToolCallID,
	}
}

// reverseMessages reverses a slice of messages to correct order
func (t *AgenticTruncator) reverseMessages(reversed []spec.PromptMessage) []spec.PromptMessage {
	out := make([]spec.PromptMessage, len(reversed))
	for i, msg := range reversed {
		out[len(reversed)-1-i] = msg
	}
	return out
}

// addCollapseMarkerIfNeeded adds collapse marker when messages were dropped
func (t *AgenticTruncator) addCollapseMarkerIfNeeded(out, originalMessages []spec.PromptMessage) []spec.PromptMessage {
	droppedUntil := len(originalMessages) - len(out)
	droppedExchanges := 0
	for _, ex := range findToolExchanges(originalMessages) {
		if ex.assistantIndex < droppedUntil {
			droppedExchanges++
		}
	}
	if droppedExchanges > 0 {
		out[0].Content = CollapseMarkerFor(droppedExchanges) + " " + out[0].Content
	}
	return out
}
