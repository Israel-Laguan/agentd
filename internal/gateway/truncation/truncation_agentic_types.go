package truncation

import (
	"agentd/internal/gateway/spec"
)

// AgenticTruncator is a truncator that handles tool exchanges and collapse markers
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

// NewAgenticTruncator creates a new AgenticTruncator with the specified max message count
func NewAgenticTruncator(maxMessages int) spec.Truncator {
	if maxMessages <= 0 {
		maxMessages = 20
	}
	return &AgenticTruncator{MaxMessages: maxMessages}
}
