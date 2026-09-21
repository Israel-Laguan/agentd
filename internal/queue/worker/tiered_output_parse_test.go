package worker

import (
	"strings"
	"testing"
)

func TestExtractJSONCandidates_PreservesDocumentOrder(t *testing.T) {
	t.Parallel()
	generic := `{"version":1,"task_id":"generic","summary":"first","paths":["a.go"]}`
	typed := `{"version":1,"task_id":"typed","summary":"second","paths":["b.go"]}`
	output := "notes\n```\n" + generic + "\n```\nmore\n```json\n" + typed + "\n```"
	candidates := extractJSONCandidates(output)
	if len(candidates) != 3 {
		t.Fatalf("len(candidates) = %d, want 3: %q", len(candidates), candidates)
	}
	if !strings.Contains(candidates[1], `"generic"`) {
		t.Errorf("candidates[1] should hold the earlier generic payload, got %q", candidates[1])
	}
	if !strings.Contains(candidates[2], `"typed"`) {
		t.Errorf("candidates[2] should hold the later json payload, got %q", candidates[2])
	}
}

func TestExtractJSONCandidates_IgnoresInlineBackticks(t *testing.T) {
	t.Parallel()
	payload := "{\"version\":1,\"task_id\":\"t1\",\"summary\":\"use ``` to fence code\",\"paths\":[\"a.go\"]}"
	output := "```json\n" + payload + "\n```"
	candidates := extractJSONCandidates(output)
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2: %q", len(candidates), candidates)
	}
	if !strings.Contains(candidates[1], "use ``` to fence") {
		t.Errorf("fenced candidate was truncated at inline backticks: %q", candidates[1])
	}
}

func TestParseVerifyResult_SkipsInvalidFirstCandidate(t *testing.T) {
	t.Parallel()
	output := "```\n" + `{"overall":"pass"}` + "\n```\n```json\n" +
		`{"results":[{"check":"test","outcome":"pass"}],"overall":"pass"}` + "\n```"
	vr, err := parseVerifyResult(output)
	if err != nil {
		t.Fatalf("parseVerifyResult() error = %v", err)
	}
	if vr.Overall != "pass" || len(vr.Results) != 1 {
		t.Errorf("unexpected VerifyResult: %+v", vr)
	}
}

func TestParseContextPack_SkipsInvalidFirstCandidate(t *testing.T) {
	t.Parallel()
	output := "```\n" + `{"version":999,"summary":"","paths":[]}` + "\n```\n```json\n" +
		`{"version":1,"task_id":"t9","summary":"second","paths":["b.go"]}` + "\n```"
	cp, err := parseContextPack(output)
	if err != nil {
		t.Fatalf("parseContextPack() error = %v", err)
	}
	if cp.TaskID != "t9" {
		t.Errorf("TaskID = %q, want %q", cp.TaskID, "t9")
	}
}

func TestParseContextPackCandidates_ReturnsAllValidCandidates(t *testing.T) {
	t.Parallel()
	cp1 := `{"version":1,"task_id":"t1","summary":"first","paths":["a.go"]}`
	cp2 := `{"version":1,"task_id":"t2","summary":"second","paths":["b.go"]}`
	output := "```json\n" + cp1 + "\n```\n```json\n" + cp2 + "\n```"
	candidates, err := parseContextPackCandidates(output)
	if err != nil {
		t.Fatalf("parseContextPackCandidates() error = %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(candidates))
	}
	if candidates[0].TaskID != "t1" {
		t.Errorf("candidates[0].TaskID = %q, want %q", candidates[0].TaskID, "t1")
	}
	if candidates[1].TaskID != "t2" {
		t.Errorf("candidates[1].TaskID = %q, want %q", candidates[1].TaskID, "t2")
	}
}

func TestParseContextPackCandidates_SkipsInvalidJSON(t *testing.T) {
	t.Parallel()
	invalid := `{"version":999,"summary":"","paths":[]}`
	valid := `{"version":1,"task_id":"t9","summary":"second","paths":["b.go"]}`
	output := "```json\n" + invalid + "\n```\n```json\n" + valid + "\n```"
	candidates, err := parseContextPackCandidates(output)
	if err != nil {
		t.Fatalf("parseContextPackCandidates() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1 (invalid candidate should be skipped)", len(candidates))
	}
	if candidates[0].TaskID != "t9" {
		t.Errorf("candidates[0].TaskID = %q, want %q", candidates[0].TaskID, "t9")
	}
}

func TestParseContextPackCandidates_NoValidCandidates(t *testing.T) {
	t.Parallel()
	output := "```json\n{\"version\":999,\"summary\":\"\",\"paths\":[]}\n```"
	_, err := parseContextPackCandidates(output)
	if err == nil {
		t.Fatal("parseContextPackCandidates() should error when no valid candidate exists")
	}
}
