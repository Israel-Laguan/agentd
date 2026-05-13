package truncation

import (
	"agentd/internal/gateway/spec"
)

// AgenticTruncator is a truncator that handles tool exchanges and collapse markers
type AgenticTruncator struct {
	MaxMessages int
}

// toolExchange represents an assistant message with tool_calls
type toolExchange struct {
	assistantIndex int
}

// findToolExchanges identifies assistant messages with tool_calls
func findToolExchanges(messages []spec.PromptMessage) []toolExchange {
	var exchanges []toolExchange

	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			exchanges = append(exchanges, toolExchange{assistantIndex: i})
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
