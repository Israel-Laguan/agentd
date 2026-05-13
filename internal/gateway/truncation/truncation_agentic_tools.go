package truncation

import (
	"agentd/internal/gateway/spec"
)

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
