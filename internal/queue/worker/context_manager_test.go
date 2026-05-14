package worker

import (
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway/spec"
)

// ---------------------------------------------------------------------------
// CorrectionRecord
// ---------------------------------------------------------------------------

func TestCorrectionRecord_FormatMessage(t *testing.T) {
	rec := CorrectionRecord{
		Contradiction: "the server runs on port 3000",
		CorrectFact:   "the server runs on port 8080",
		Source:        CorrectionSourceTool,
	}
	msg := rec.FormatMessage()
	if !strings.HasPrefix(msg, "[CORRECTION]") {
		t.Fatalf("expected [CORRECTION] prefix, got %q", msg)
	}
	if !strings.Contains(msg, "the server runs on port 3000") {
		t.Fatal("expected contradiction text in message")
	}
	if !strings.Contains(msg, "the server runs on port 8080") {
		t.Fatal("expected correct fact text in message")
	}
}

// ---------------------------------------------------------------------------
// InjectCorrection — basic prepend behavior
// ---------------------------------------------------------------------------

func TestContextManager_InjectCorrection_PrependsToWorkingZone(t *testing.T) {
	seed := []spec.PromptMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "do the thing"},
	}
	cm := NewContextManager(seed)

	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "old fact",
		CorrectFact:   "new fact",
		Source:        CorrectionSourceHuman,
	})

	msgs := cm.WorkingMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if !IsCorrectionMessage(msgs[0].Content) {
		t.Fatalf("first message should be a correction, got %q", msgs[0].Content)
	}
	if msgs[0].Role != "system" {
		t.Fatalf("correction message should have role=system, got %q", msgs[0].Role)
	}
}

// ---------------------------------------------------------------------------
// Multiple corrections stack newest-first
// ---------------------------------------------------------------------------

func TestContextManager_MultipleCorrections_NewestFirst(t *testing.T) {
	cm := NewContextManager([]spec.PromptMessage{
		{Role: "user", Content: "hello"},
	})

	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "first",
		CorrectFact:   "corrected-first",
		Source:        CorrectionSourceTool,
	})
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "second",
		CorrectFact:   "corrected-second",
		Source:        CorrectionSourceHuman,
	})

	msgs := cm.WorkingMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (2 corrections + 1 seed), got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "second") {
		t.Fatalf("newest correction should be first, got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "first") {
		t.Fatalf("older correction should be second, got %q", msgs[1].Content)
	}

	corrections := cm.Corrections()
	if len(corrections) != 2 {
		t.Fatalf("expected 2 correction records, got %d", len(corrections))
	}
	if corrections[0].Contradiction != "second" {
		t.Fatalf("corrections should be newest-first, got %q", corrections[0].Contradiction)
	}
}

// ---------------------------------------------------------------------------
// InjectHumanCorrection convenience method
// ---------------------------------------------------------------------------

func TestContextManager_InjectHumanCorrection(t *testing.T) {
	cm := NewContextManager(nil)
	cm.InjectHumanCorrection("stale fact", "current fact")

	corrections := cm.Corrections()
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Source != CorrectionSourceHuman {
		t.Fatalf("expected human source, got %q", corrections[0].Source)
	}
	if corrections[0].Contradiction != "stale fact" {
		t.Fatalf("unexpected contradiction: %q", corrections[0].Contradiction)
	}
	if corrections[0].CorrectFact != "current fact" {
		t.Fatalf("unexpected correct fact: %q", corrections[0].CorrectFact)
	}
}

// ---------------------------------------------------------------------------
// AddSummary + Messages ordering
// ---------------------------------------------------------------------------

func TestContextManager_Messages_CompressedThenWorking(t *testing.T) {
	cm := NewContextManager([]spec.PromptMessage{
		{Role: "user", Content: "working message"},
	})
	cm.AddSummary(TurnSummary{
		Summary:          "summary of turns 1-5",
		FactsEstablished: []string{"port=3000"},
	})

	msgs := cm.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "summary of turns 1-5" {
		t.Fatalf("compressed zone should come first, got %q", msgs[0].Content)
	}
	if msgs[1].Content != "working message" {
		t.Fatalf("working zone should come second, got %q", msgs[1].Content)
	}
}

// ---------------------------------------------------------------------------
// CheckToolResult — auto-detect contradictions
// ---------------------------------------------------------------------------

func TestContextManager_CheckToolResult_DetectsContradiction(t *testing.T) {
	cm := NewContextManager([]spec.PromptMessage{
		{Role: "user", Content: "query"},
	})
	cm.AddSummary(TurnSummary{
		Summary:          "Server configured on port 3000",
		FactsEstablished: []string{"port=3000"},
	})

	detected := cm.CheckToolResult("port=8080")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction, got %d", len(detected))
	}
	if detected[0].Source != CorrectionSourceTool {
		t.Fatalf("expected tool source, got %q", detected[0].Source)
	}

	msgs := cm.WorkingMessages()
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages after correction, got %d", len(msgs))
	}
	if !IsCorrectionMessage(msgs[0].Content) {
		t.Fatal("first working message should be a correction")
	}
}

func TestContextManager_CheckToolResult_NoContradiction(t *testing.T) {
	cm := NewContextManager(nil)
	cm.AddSummary(TurnSummary{
		Summary:          "Server on port 3000",
		FactsEstablished: []string{"port=3000"},
	})

	detected := cm.CheckToolResult("port=3000")
	if len(detected) != 0 {
		t.Fatalf("expected no contradictions for matching value, got %d", len(detected))
	}
}

// ---------------------------------------------------------------------------
// IsCorrectionMessage
// ---------------------------------------------------------------------------

func TestIsCorrectionMessage(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "valid correction", content: "[CORRECTION] Earlier context may state: ...", want: true},
		{name: "leading whitespace", content: "  [CORRECTION] test", want: true},
		{name: "not a correction", content: "some regular message", want: false},
		{name: "empty string", content: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCorrectionMessage(tt.content); got != tt.want {
				t.Fatalf("IsCorrectionMessage(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AppendWorking
// ---------------------------------------------------------------------------

func TestContextManager_AppendWorking(t *testing.T) {
	cm := NewContextManager([]spec.PromptMessage{
		{Role: "user", Content: "first"},
	})
	cm.AppendWorking(spec.PromptMessage{Role: "assistant", Content: "reply"})

	msgs := cm.WorkingMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[1].Content != "reply" {
		t.Fatalf("expected appended message at tail, got %q", msgs[1].Content)
	}
}

// ---------------------------------------------------------------------------
// Timestamp auto-fill
// ---------------------------------------------------------------------------

func TestInjectCorrection_AutoFillsTimestamp(t *testing.T) {
	cm := NewContextManager(nil)
	before := time.Now()
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "a",
		CorrectFact:   "b",
		Source:        CorrectionSourceTool,
	})
	after := time.Now()

	rec := cm.Corrections()[0]
	if rec.Timestamp.Before(before) || rec.Timestamp.After(after) {
		t.Fatalf("timestamp %v should be between %v and %v", rec.Timestamp, before, after)
	}
}
