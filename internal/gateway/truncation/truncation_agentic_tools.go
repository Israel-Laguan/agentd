package truncation

import (
	"agentd/internal/gateway/spec"
)

// removeDanglingToolCalls ensures pairwise consistency:
// - If an assistant message has ToolCalls, ensure corresponding tool responses exist
// - If not, mark the ToolCalls as collapsed instead of leaving orphans
// - Also remove tool responses that don't have corresponding tool calls (orphaned tool responses)
func (t *AgenticTruncator) removeDanglingToolCalls(messages []spec.PromptMessage) []spec.PromptMessage {
	if len(messages) == 0 {
		return messages
	}

	// First, build a set of valid tool call IDs from assistant messages
	validToolCallIDs := make(map[string]bool)
	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			for _, tc := range messages[i].ToolCalls {
				validToolCallIDs[tc.ID] = true
			}
		}
	}

	// Filter out orphaned tool responses (tool messages whose ToolCallID doesn't match any assistant's ToolCalls)
	out := make([]spec.PromptMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "tool" {
			// Keep tool response only if its ToolCallID exists in validToolCallIDs
			if validToolCallIDs[msg.ToolCallID] {
				out = append(out, msg)
			}
			// Otherwise, it's orphaned - skip it
		} else {
			out = append(out, msg)
		}
	}

	// Now handle assistant messages with orphan tool_calls (tool_calls without corresponding tool responses)
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
				// Add collapse marker to the content and only keep tool calls that have corresponding responses
				if out[i].Content != "" {
					out[i].Content = out[i].Content + " " + CollapseMarker
				} else {
					out[i].Content = CollapseMarker
				}
				// Filter to keep only tool calls that have corresponding tool responses
				var validToolCalls []spec.ToolCall
				for _, tc := range out[i].ToolCalls {
					hasResponse := false
					for j := i + 1; j < len(out); j++ {
						if out[j].Role == "tool" && out[j].ToolCallID == tc.ID {
							hasResponse = true
							break
						}
					}
					if hasResponse {
						validToolCalls = append(validToolCalls, tc)
					}
				}
				out[i].ToolCalls = validToolCalls
			}
		}
	}

	return out
}
