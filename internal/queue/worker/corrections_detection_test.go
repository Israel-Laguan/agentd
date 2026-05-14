package worker

import (
	"strings"
	"testing"
)

func TestDetectContradictions_BooleanFlip(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"feature is enabled"}},
	}
	detected := DetectContradictions(summaries, "The feature is disabled")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for boolean flip, got %d", len(detected))
	}
	if !strings.Contains(detected[0].CorrectFact, "disabled") {
		t.Fatalf("expected 'disabled' in correct fact, got %q", detected[0].CorrectFact)
	}
}

func TestDetectContradictions_BooleanFlip_NoDuplicateVerb(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"the server is enabled"}},
	}
	detected := DetectContradictions(summaries, "The server is disabled")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for boolean flip, got %d", len(detected))
	}
	if detected[0].CorrectFact != "the server is disabled" {
		t.Fatalf("expected no duplicate verb, got %q", detected[0].CorrectFact)
	}
}

func TestDetectContradictions_BooleanFlip_RemovesOnlyMatchedToken(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"running service is running"}},
	}
	detected := DetectContradictions(summaries, "Running service is stopped")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for boolean flip, got %d", len(detected))
	}
	if detected[0].CorrectFact != "running service is stopped" {
		t.Fatalf("expected first token to remain in subject, got %q", detected[0].CorrectFact)
	}
}

func TestDetectContradictions_ProseValueChange_MidFactBooleanToken(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"flag is enabled for admin"}},
	}
	detected := DetectContradictions(summaries, "Flag is disabled for admin")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for prose value change, got %d", len(detected))
	}
	if detected[0].CorrectFact != "flag is disabled for admin" {
		t.Fatalf("expected updated fact, got %q", detected[0].CorrectFact)
	}
}

func TestDetectContradictions_BooleanFlip_DoesNotCrossCompoundClause(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"feature is enabled"}},
	}
	detected := DetectContradictions(summaries, "Feature is enabled and logging is disabled")
	if len(detected) != 0 {
		t.Fatalf("expected no contradiction from unrelated clause, got %d", len(detected))
	}
}

func TestDetectContradictions_BooleanFlip_RequiresSubjectBoundary(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"port is enabled"}},
	}
	detected := DetectContradictions(summaries, "Support is disabled")
	if len(detected) != 0 {
		t.Fatalf("expected no contradiction for embedded subject match, got %d", len(detected))
	}
}

func TestDetectContradictions_ChangedPattern(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"Port is 3000"}},
	}
	detected := DetectContradictions(summaries, "Port changed to 8080")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for changed pattern, got %d", len(detected))
	}
	if !strings.Contains(detected[0].CorrectFact, "8080") {
		t.Fatalf("expected '8080' in correct fact, got %q", detected[0].CorrectFact)
	}
}

func TestDetectContradictions_ChangedPattern_RequiresSubjectBoundary(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"support is enabled"}},
	}
	detected := DetectContradictions(summaries, "Port changed to 8080")
	if len(detected) != 0 {
		t.Fatalf("expected no contradiction for subject substring match, got %d", len(detected))
	}
}

func TestDetectContradictions_ChangedPattern_TrimsTrailingOutput(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"Version is 1.0.0"}},
	}
	detected := DetectContradictions(summaries, "Version updated to 2.0.0\nStarting service...")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for changed pattern, got %d", len(detected))
	}
	if detected[0].CorrectFact != "version is 2.0.0" {
		t.Fatalf("expected value bounded to first line, got %q", detected[0].CorrectFact)
	}
}

func TestDetectContradictions_ChangedPattern_ScansMultipleChanges(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"Memory is 8gb"}},
	}
	detected := DetectContradictions(summaries, "Port changed to 8080. Memory changed to 16gb")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for second changed pattern, got %d", len(detected))
	}
	if detected[0].CorrectFact != "memory is 16gb" {
		t.Fatalf("unexpected correct fact: %q", detected[0].CorrectFact)
	}
}

func TestLastTokenIndex_UnicodeBoundary(t *testing.T) {
	if idx := lastTokenIndex("décafé", "café"); idx >= 0 {
		t.Fatalf("expected no embedded unicode token match, got %d", idx)
	}
	if idx := lastTokenIndex("le café", "café"); idx < 0 {
		t.Fatal("expected standalone unicode token match")
	}
}

func TestDetectContradictions_ProseValueChange(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"The server runs on port 3000"}},
	}
	detected := DetectContradictions(summaries, "The server runs on port 8080")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for prose value change, got %d", len(detected))
	}
	if !strings.Contains(detected[0].CorrectFact, "8080") {
		t.Fatalf("expected '8080' in correct fact, got %q", detected[0].CorrectFact)
	}
}

func TestDetectContradictions_ProseValueChange_RequiresSubjectBoundary(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"port uses tcp"}},
	}
	detected := DetectContradictions(summaries, "Support uses udp")
	if len(detected) != 0 {
		t.Fatalf("expected no contradiction for embedded subject match, got %d", len(detected))
	}
}
