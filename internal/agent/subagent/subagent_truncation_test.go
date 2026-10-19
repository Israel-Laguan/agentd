package subagent

import (
	"strings"
	"testing"

	"agentd/internal/gateway"
)

// A subagent trims its context with the strategy its delegate was given,
// like the parent's ContextManager, and passes it to nested delegates.
func TestEnforceSubagentContextBudget_UsesStrategy(t *testing.T) {
	strategy := gateway.HeadTailStrategy{HeadRatio: 0.8}
	long := strings.Repeat("h", 500) + strings.Repeat("t", 500)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: long},
	}

	got := enforceSubagentContextBudget(messages, len("system")+len("task")+200, strategy)

	if want := strategy.Truncate(long, 200); got[2].Content != want {
		t.Fatalf("subagent working message = %q, want head_tail cut %q", got[2].Content, want)
	}
}

func TestSubagentChild_InheritsTruncationStrategy(t *testing.T) {
	strategy := gateway.HeadTailStrategy{HeadRatio: 0.8}
	parent := NewSubagentDelegate(nil, nil, "/ws", nil, 0, 0).WithTruncationStrategy(strategy)

	child := parent.child()

	if child.truncation != strategy {
		t.Fatalf("child truncation = %#v, want %#v", child.truncation, strategy)
	}
	if child.depth != 1 {
		t.Fatalf("child depth = %d, want 1", child.depth)
	}
}
