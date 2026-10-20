package truncation

import (
	"unicode/utf8"

	"agentd/internal/gateway/spec"
)

// collectValidToolCallIDs builds a set of tool call IDs from assistant messages.
func collectValidToolCallIDs(messages []spec.PromptMessage) map[string]bool {
	valid := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				valid[tc.ID] = true
			}
		}
	}
	return valid
}

// filterOrphanedToolResponses removes tool responses that don't have corresponding tool calls.
func filterOrphanedToolResponses(messages []spec.PromptMessage, validIDs map[string]bool) []spec.PromptMessage {
	out := make([]spec.PromptMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "tool" {
			if validIDs[msg.ToolCallID] {
				out = append(out, msg)
			}
		} else {
			out = append(out, msg)
		}
	}
	return out
}

// hasOrphanToolCalls checks if any tool calls in the given message lack corresponding tool responses later in the list.
func hasOrphanToolCalls(msg spec.PromptMessage, messages []spec.PromptMessage, startIdx int) bool {
	for _, tc := range msg.ToolCalls {
		found := false
		for j := startIdx + 1; j < len(messages); j++ {
			if messages[j].Role == "tool" && messages[j].ToolCallID == tc.ID {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// filterValidToolCalls keeps only tool calls that have corresponding tool responses.
func filterValidToolCalls(calls []spec.ToolCall, messages []spec.PromptMessage, startIdx int) []spec.ToolCall {
	var valid []spec.ToolCall
	for _, tc := range calls {
		for j := startIdx + 1; j < len(messages); j++ {
			if messages[j].Role == "tool" && messages[j].ToolCallID == tc.ID {
				valid = append(valid, tc)
				break
			}
		}
	}
	return valid
}

// markOrphanToolCalls adds a collapse marker to content and filters orphan tool calls.
func markOrphanToolCalls(msg spec.PromptMessage) spec.PromptMessage {
	if msg.Content != "" {
		msg.Content = msg.Content + " " + CollapseMarker
	} else {
		msg.Content = CollapseMarker
	}
	return msg
}

// removeDanglingToolCalls ensures pairwise consistency:
// - If an assistant message has ToolCalls, ensure corresponding tool responses exist
// - If not, mark the ToolCalls as collapsed instead of leaving orphans
// - Also remove tool responses that don't have corresponding tool calls (orphaned tool responses)
func (t *AgenticTruncator) removeDanglingToolCalls(messages []spec.PromptMessage) []spec.PromptMessage {
	if len(messages) == 0 {
		return messages
	}

	validIDs := collectValidToolCallIDs(messages)
	out := filterOrphanedToolResponses(messages, validIDs)

	for i := 0; i < len(out); i++ {
		if out[i].Role == "assistant" && len(out[i].ToolCalls) > 0 {
			if !hasOrphanToolCalls(out[i], out, i) {
				continue
			}
			out[i] = markOrphanToolCalls(out[i])
			out[i].ToolCalls = filterValidToolCalls(out[i].ToolCalls, out, i)
		}
	}

	return out
}

// messageChars is a message's content plus its tool call names and arguments.
func messageChars(msg spec.PromptMessage) int {
	n := utf8.RuneCountInString(msg.Content)
	for _, tc := range msg.ToolCalls {
		n += utf8.RuneCountInString(tc.Function.Name)
		n += utf8.RuneCountInString(tc.Function.Arguments)
	}
	return n
}

// pairedCall finds the nearest earlier assistant message that calls callID
// and returns its index and size, or -1 when there is none.
func pairedCall(earlier []spec.PromptMessage, callID string) (int, int) {
	for i := len(earlier) - 1; i >= 0 && callID != ""; i-- {
		for _, tc := range earlier[i].ToolCalls {
			if tc.ID == callID {
				return i, messageChars(earlier[i])
			}
		}
	}
	return -1, 0
}
