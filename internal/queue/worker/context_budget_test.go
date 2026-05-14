package worker

import (
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway/spec"
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
