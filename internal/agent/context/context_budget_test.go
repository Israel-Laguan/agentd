package context

import (
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

func TestEnforceBudget_DoesNotCorruptMessagesBetweenCorrectionsAndSummary(t *testing.T) {
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", "task")
	correction := CorrectionRecord{
		Contradiction: "stale",
		CorrectFact:   "current",
		Source:        CorrectionSourceHuman,
	}.FormatMessage()
	summary := "PREVIOUS CONTEXT SUMMARY\n- old work"
	intervening := "keep me in working"
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "system", Content: correction},
		{Role: "assistant", Content: intervening},
		{Role: "system", Content: summary},
		{Role: "assistant", Content: strings.Repeat("x", 1000)},
	}

	prepared := cm.enforceBudget(messages, len("system")+len("task")+len(correction)+len(summary)+len(intervening))
	if len(prepared) != 5 {
		t.Fatalf("expected anchor, correction, summary, and intervening message, got %#v", prepared)
	}
	if prepared[3].Content != summary {
		t.Fatalf("expected protected summary before working messages, got %#v", prepared)
	}
	if prepared[4].Content != intervening {
		t.Fatalf("expected uncorrupted intervening message in working set, got %#v", prepared)
	}
}

// gateway.truncation.strategy reaches the working zone: the configured
// strategy, not a hardcoded middle-out, cuts what exceeds the budget.
func TestEnforceBudget_UsesConfiguredTruncationStrategy(t *testing.T) {
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", "task")
	strategy := truncation.HeadTailStrategy{HeadRatio: 0.8}
	cm.SetTruncationStrategy(strategy)
	long := strings.Repeat("h", 500) + strings.Repeat("t", 500)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: long},
	}

	prepared := cm.enforceBudget(messages, len("system")+len("task")+200)

	want := strategy.Truncate(long, 200)
	if want == (truncation.MiddleOutStrategy{}).Truncate(long, 200) {
		t.Fatal("test input does not tell head_tail from middle_out")
	}
	if got := prepared[len(prepared)-1].Content; got != want {
		t.Fatalf("working message = %q, want head_tail cut %q", got, want)
	}
}

func TestTruncationStrategy_DefaultsToMiddleOut(t *testing.T) {
	var unset *ContextManager
	for _, cm := range []*ContextManager{unset, {}, NewContextManager(config.AgenticContextConfig{}, nil, "a", "t")} {
		if _, ok := cm.TruncationStrategy().(truncation.MiddleOutStrategy); !ok {
			t.Fatalf("TruncationStrategy() = %T, want MiddleOutStrategy", cm.TruncationStrategy())
		}
	}
}
