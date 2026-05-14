package worker

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// DetectContradictions
// ---------------------------------------------------------------------------

func TestDetectContradictions_NegationPattern(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"server is running"}},
	}
	detected := DetectContradictions(summaries, "The server is not running")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction, got %d", len(detected))
	}
	if detected[0].Contradiction != "server is running" {
		t.Fatalf("unexpected contradiction: %q", detected[0].Contradiction)
	}
	if detected[0].Source != CorrectionSourceTool {
		t.Fatalf("expected tool source, got %q", detected[0].Source)
	}
}

func TestDetectContradictions_NoLongerPattern(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"active"}},
	}
	detected := DetectContradictions(summaries, "The process is no longer active")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction, got %d", len(detected))
	}
}

func TestDetectContradictions_IsntPattern(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"available"}},
	}
	detected := DetectContradictions(summaries, "Service isn't available")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction, got %d", len(detected))
	}
}

func TestDetectContradictions_KeyValueConflict(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"version=1.2.3"}},
	}
	detected := DetectContradictions(summaries, "version=2.0.0")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction, got %d", len(detected))
	}
	if detected[0].CorrectFact != "version=2.0.0" {
		t.Fatalf("expected correct fact 'version=2.0.0', got %q", detected[0].CorrectFact)
	}
}

func TestDetectContradictions_KeyValueMatch_NoContradiction(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"version=1.2.3"}},
	}
	detected := DetectContradictions(summaries, "version=1.2.3")
	if len(detected) != 0 {
		t.Fatalf("expected no contradictions for matching key-value, got %d", len(detected))
	}
}

func TestDetectContradictions_EmptyToolOutput(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"some fact"}},
	}
	detected := DetectContradictions(summaries, "")
	if len(detected) != 0 {
		t.Fatalf("expected no contradictions for empty output, got %d", len(detected))
	}
}

func TestDetectContradictions_EmptyFact_Skipped(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{""}},
	}
	detected := DetectContradictions(summaries, "not ")
	if len(detected) != 0 {
		t.Fatalf("expected no contradictions for empty fact, got %d", len(detected))
	}
}

func TestDetectContradictions_MultipleSummaries(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"port=3000"}},
		{FactsEstablished: []string{"host=localhost"}},
	}
	detected := DetectContradictions(summaries, "port=8080\nhost=localhost")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction (port changed, host same), got %d", len(detected))
	}
	if detected[0].Contradiction != "port=3000" {
		t.Fatalf("expected port contradiction, got %q", detected[0].Contradiction)
	}
}

func TestDetectContradictions_CaseInsensitive(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"Server is Running"}},
	}
	detected := DetectContradictions(summaries, "server is not running")
	if len(detected) != 1 {
		t.Fatalf("expected case-insensitive match, got %d contradictions", len(detected))
	}
}

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

func TestDetectContradictions_BooleanFlip_NormalizesSubjectSpacing(t *testing.T) {
	summaries := []TurnSummary{
		{FactsEstablished: []string{"flag is enabled for admin"}},
	}
	detected := DetectContradictions(summaries, "Flag is disabled for admin")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction for boolean flip, got %d", len(detected))
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

// ---------------------------------------------------------------------------
// extractKeyValues
// ---------------------------------------------------------------------------

func TestExtractKeyValues_EqualsSign(t *testing.T) {
	kv := extractKeyValues("port=8080\nhost=example.com")
	if kv["port"] != "8080" {
		t.Fatalf("expected port=8080, got %q", kv["port"])
	}
	if kv["host"] != "example.com" {
		t.Fatalf("expected host=example.com, got %q", kv["host"])
	}
}

func TestExtractKeyValues_ColonSeparator(t *testing.T) {
	kv := extractKeyValues("status: active\nmode: production")
	if kv["status"] != "active" {
		t.Fatalf("expected status=active, got %q", kv["status"])
	}
}

func TestExtractKeyValues_EmptyInput(t *testing.T) {
	kv := extractKeyValues("")
	if len(kv) != 0 {
		t.Fatalf("expected empty map for empty input, got %d entries", len(kv))
	}
}

// ---------------------------------------------------------------------------
// extractCorrectFact
// ---------------------------------------------------------------------------

func TestExtractCorrectFact_ReturnsNegationLine(t *testing.T) {
	output := "Checking status...\nService is not running\nDone."
	result := extractCorrectFact(output, "running")
	if result != "Service is not running" {
		t.Fatalf("expected negation line, got %q", result)
	}
}

func TestExtractCorrectFact_FallsBackToFullOutput(t *testing.T) {
	output := "some unrelated output"
	result := extractCorrectFact(output, "running")
	if result != output {
		t.Fatalf("expected full output as fallback, got %q", result)
	}
}
