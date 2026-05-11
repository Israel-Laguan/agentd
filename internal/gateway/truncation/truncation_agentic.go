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
			for startFrom < len(messages) && messages[startFrom].Role == "tool" {
				startFrom++
				truncated = true
			}

			if startFrom < len(messages) && truncated {
				msg := messages[startFrom]
				msg.Content = TruncationMarker + msg.Content
				out = append(out, msg)
				startFrom++
			}
			if startFrom < len(messages) {
				out = append(out, messages[startFrom:]...)
			}
		}
	}

	if len(out) > 0 {
		last := out[len(out)-1]
		if last.Role == "assistant" && len(last.ToolCalls) > 0 {
			out = out[:len(out)-1]
		}
	}

	return out, nil
}
