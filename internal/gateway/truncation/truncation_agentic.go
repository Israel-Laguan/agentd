package truncation

import (
	"context"

	"agentd/internal/gateway/spec"
)

type AgenticTruncator struct {
	MaxMessages int
}

func NewAgenticTruncator(maxMessages int) spec.Truncator {
	if maxMessages <= 0 {
		maxMessages = 20
	}
	return &AgenticTruncator{MaxMessages: maxMessages}
}

func (t *AgenticTruncator) Apply(_ context.Context, messages []spec.PromptMessage, _ int) ([]spec.PromptMessage, error) {
	if len(messages) <= t.MaxMessages {
		return messages, nil
	}

	out := make([]spec.PromptMessage, 0, t.MaxMessages)

	out = append(out, messages[0])

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

	if len(out) < t.MaxMessages {
		remaining := t.MaxMessages - len(out)
		startIdx := firstUserIdx + 1
		if startIdx < len(messages) {
			endIdx := startIdx + remaining
			if endIdx > len(messages) {
				endIdx = len(messages)
			}
			out = append(out, messages[startIdx:endIdx]...)
		}
	}

	if len(out) < t.MaxMessages && len(messages) > t.MaxMessages {
		lastMsg := messages[len(messages)-1]
		lastMsg.Content = TruncationMarker + lastMsg.Content
		out = append(out, lastMsg)
	}

	return out, nil
}
