package truncation

import (
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestMiddleOutStrategyMatchesMiddleOut(t *testing.T) {
	input := "abcdefghijklmnopqrstuvwxyz"
	budget := 20
	got := MiddleOutStrategy{}.Truncate(input, budget)
	want := MiddleOut(input, budget)
	if got != want {
		t.Fatalf("MiddleOutStrategy.Truncate() = %q, want %q", got, want)
	}
}

func TestTruncateMessagesUsesProvidedStrategy(t *testing.T) {
	input := strings.Repeat("a", 50) + strings.Repeat("b", 50)
	messages := []spec.PromptMessage{{Role: "user", Content: input}}
	got := truncateMessages(messages, 40, HeadTailStrategy{HeadRatio: 1})
	if got[0].Content != input[:40-len(truncationMarker)]+truncationMarker {
		t.Fatalf("truncateMessages() = %q, want head-only truncation", got[0].Content)
	}
	if messages[0].Content != input {
		t.Fatalf("truncateMessages() mutated original message")
	}
}
