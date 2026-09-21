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
