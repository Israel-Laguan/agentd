package worker

import "testing"

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
