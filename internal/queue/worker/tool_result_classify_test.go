package worker

import (
	"strings"
	"testing"

	"agentd/internal/sandbox"
)

func TestClassifyRawResult_SuccessPlainText(t *testing.T) {
	t.Parallel()
	tr := classifyRawResult("c1", "hello world", 10)
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("Status = %s, want success", tr.Status)
	}
	if tr.Content != "hello world" {
		t.Fatalf("Content = %q, want hello world", tr.Content)
	}
}

func TestClassifyRawResult_JSONError(t *testing.T) {
	t.Parallel()
	tr := classifyRawResult("c1", `{"error":"file not found"}`, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.Error == nil || tr.Error.Message != "file not found" {
		t.Fatalf("Error.Message = %v, want file not found", tr.Error)
	}
	if tr.Retryable {
		t.Fatal("JSON error results should be non-retryable by default")
	}
}

func TestClassifyRawResult_JSONFatal(t *testing.T) {
	t.Parallel()
	tr := classifyRawResult("c1", `{"FatalError":"crash"}`, 10)
	if tr.Status != ToolStatusFatal {
		t.Fatalf("Status = %s, want fatal", tr.Status)
	}
}

func TestClassifyRawResult_JSONTimeout(t *testing.T) {
	t.Parallel()
	tr := classifyRawResult("c1", `{"status":"timeout"}`, 5000)
	if tr.Status != ToolStatusTimeout {
		t.Fatalf("Status = %s, want timeout", tr.Status)
	}
}

func TestClassifyRawResult_JSONSuccessFalse(t *testing.T) {
	t.Parallel()
	raw := `{"Success":false,"ExitCode":1}`
	tr := classifyRawResult("c1", raw, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.Retryable {
		t.Fatal("non-retryable error should have Retryable=false")
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want raw JSON preserved %q", tr.Content, raw)
	}
	if tr.Error == nil || !strings.Contains(tr.Error.Message, "exit code 1") {
		t.Fatalf("Error.Message = %v, want exit code summary", tr.Error)
	}
	if !tr.ExitCodeSet || tr.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, ExitCodeSet = %v, want 1/true", tr.ExitCode, tr.ExitCodeSet)
	}
}

func TestClassifyRawResult_JSONSuccessFalseNoExitCode(t *testing.T) {
	t.Parallel()
	raw := `{"Success":false}`
	tr := classifyRawResult("c1", raw, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.ExitCodeSet {
		t.Fatalf("ExitCodeSet = true, want false when ExitCode is absent")
	}
	if got := toolResultExitCode(tr); got != -1 {
		t.Fatalf("toolResultExitCode() = %d, want -1", got)
	}
	if tr.Error == nil || tr.Error.Message != "command failed" {
		t.Fatalf("Error.Message = %v, want generic failure message", tr.Error)
	}
}

func TestParseToolExitCode_SuccessFalseNoExitCode(t *testing.T) {
	t.Parallel()
	if got := parseToolExitCode(`{"Success":false}`); got != -1 {
		t.Fatalf("parseToolExitCode() = %d, want -1", got)
	}
}

func TestClassifyRawResult_JSONSuccessFalsePreservesStdoutStderr(t *testing.T) {
	t.Parallel()
	raw := `{"Success":false,"ExitCode":1,"Stdout":"partial output","Stderr":"boom"}`
	tr := classifyRawResult("c1", raw, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want raw JSON preserved", tr.Content)
	}
	got := tr.ForContext()
	if !strings.Contains(got, "boom") {
		t.Fatalf("ForContext() = %q, want stderr in model context", got)
	}
	if !strings.Contains(got, "partial output") {
		t.Fatalf("ForContext() = %q, want stdout in model context", got)
	}
}

func TestClassifyRawResult_JSONSuccessTrue(t *testing.T) {
	t.Parallel()
	tr := classifyRawResult("c1", `{"Success":true,"ExitCode":0}`, 10)
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("Status = %s, want success", tr.Status)
	}
}

func TestClassifyRawResult_MalformedJSONWithErrorPrefix(t *testing.T) {
	t.Parallel()
	tr := classifyRawResult("c1", `{"error":broken`, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
}

func TestClassifyRawResult_MalformedJSONWithFatalPrefix(t *testing.T) {
	t.Parallel()
	tr := classifyRawResult("c1", `{"FatalError":broken`, 10)
	if tr.Status != ToolStatusFatal {
		t.Fatalf("Status = %s, want fatal", tr.Status)
	}
}

func TestClassifyRawResult_MalformedJSONWithLeadingWhitespace(t *testing.T) {
	t.Parallel()
	tr := classifyRawResult("c1", "\n {\"error\":broken", 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
}

func TestClassifyDelegateRawResult_JSONErrorEnvelope(t *testing.T) {
	t.Parallel()
	tr := classifyDelegateRawResult("c1", `{"error":"delegation failed: boom"}`, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.Error == nil || tr.Error.Message != "delegation failed: boom" {
		t.Fatalf("Error.Message = %v, want delegation failed: boom", tr.Error)
	}
}

func TestClassifyDelegateRawResult_SubagentSuccess(t *testing.T) {
	t.Parallel()
	raw := `{"status":"success","output":"done","iterations":2}`
	tr := classifyDelegateRawResult("c1", raw, 10)
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("Status = %s, want success", tr.Status)
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want raw JSON preserved", tr.Content)
	}
}

func TestClassifyDelegateRawResult_SubagentFailure(t *testing.T) {
	t.Parallel()
	raw := `{"status":"failure","error":"task failed","iterations":1}`
	tr := classifyDelegateRawResult("c1", raw, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.Error == nil || tr.Error.Message != "task failed" {
		t.Fatalf("Error.Message = %v, want task failed", tr.Error)
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want raw JSON preserved", tr.Content)
	}
}

func TestClassifyDelegateRawResult_SubagentTimeout(t *testing.T) {
	t.Parallel()
	raw := `{"status":"timeout","error":"max iterations reached","iterations":20}`
	tr := classifyDelegateRawResult("c1", raw, 10)
	if tr.Status != ToolStatusTimeout {
		t.Fatalf("Status = %s, want timeout", tr.Status)
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want raw JSON preserved", tr.Content)
	}
	if !tr.Retryable {
		t.Error("Retryable = false, want true")
	}
	got := tr.ForContext()
	if !strings.Contains(got, "iterations") {
		t.Fatalf("ForContext() = %q, want structured JSON in model context", got)
	}
}

func TestClassifyDelegateRawResult_ParallelWithFailure(t *testing.T) {
	t.Parallel()
	raw := `[{"status":"success","output":"ok","iterations":1},{"status":"failure","error":"parallel fail","iterations":1}]`
	tr := classifyDelegateRawResult("c1", raw, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.Error == nil || tr.Error.Message != "parallel fail" {
		t.Fatalf("Error.Message = %v, want parallel fail", tr.Error)
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want full batch JSON preserved", tr.Content)
	}
}

func TestClassifyCapabilityRawResult_JSONErrorEnvelope(t *testing.T) {
	t.Parallel()
	tr := classifyCapabilityRawResult("c1", `{"error":"unknown tool: x"}`, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
}

func TestClassifyBuiltinToolResult_ReadArbitraryJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{"fatal_error_key", `{"FatalError":"crash"}`},
		{"multi_key_error", `{"error":"invalid_grant","error_description":"token expired"}`},
		{"success_false", `{"Success":false,"ExitCode":1}`},
		{"status_timeout", `{"status":"timeout"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := classifyBuiltinToolResult("c1", toolNameRead, tc.raw, 10)
			if tr.Status != ToolStatusSuccess {
				t.Fatalf("Status = %s, want success", tr.Status)
			}
			if tr.Content != tc.raw {
				t.Fatalf("Content = %q, want raw preserved %q", tr.Content, tc.raw)
			}
			if strings.Contains(tr.ForContext(), "[ERROR]") || strings.Contains(tr.ForContext(), "[FATAL]") {
				t.Fatalf("ForContext() = %q, want unprefixed file content", tr.ForContext())
			}
		})
	}
}

func TestClassifyBuiltinToolResult_ReadJSONErrorEnvelopeIsSuccess(t *testing.T) {
	t.Parallel()
	raw := `{"error":"file not found"}`
	tr := classifyBuiltinToolResult("c1", toolNameRead, raw, 10)
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("Status = %s, want success (file content, not tool error)", tr.Status)
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want raw file bytes preserved", tr.Content)
	}
	if strings.Contains(tr.ForContext(), "[ERROR]") {
		t.Fatalf("ForContext() = %q, want unprefixed file content", tr.ForContext())
	}
}

func TestClassifyBuiltinToolResult_ReadPrefixedToolError(t *testing.T) {
	t.Parallel()
	raw := jsonErrorf("file not found")
	tr := classifyBuiltinToolResult("c1", toolNameRead, raw, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.Error == nil || tr.Error.Message != "file not found" {
		t.Fatalf("Error.Message = %v, want file not found", tr.Error)
	}
}

func TestClassifyBuiltinToolResult_BashStdoutJSONErrorEnvelope(t *testing.T) {
	t.Parallel()
	raw := `{"error":"some text"}`
	tr := classifyBuiltinToolResult("c1", toolNameBash, raw, 10)
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("Status = %s, want success (stdout, not tool error)", tr.Status)
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want raw stdout preserved", tr.Content)
	}
}

func TestClassifyBuiltinToolResult_BashPrefixedToolError(t *testing.T) {
	t.Parallel()
	tr := classifyBuiltinToolResult("c1", toolNameBash, jsonErrorf("execution failed: boom"), 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
}

func TestClassifyBuiltinToolResult_BashStdoutArbitraryJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{"fatal_error_key", `{"FatalError":"crash"}`},
		{"multi_key_error", `{"error":"invalid_grant","error_description":"token expired"}`},
		{"success_false", `{"Success":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := classifyBuiltinToolResult("c1", toolNameBash, tc.raw, 10)
			if tr.Status != ToolStatusSuccess {
				t.Fatalf("Status = %s, want success", tr.Status)
			}
			if tr.Content != tc.raw {
				t.Fatalf("Content = %q, want raw preserved %q", tr.Content, tc.raw)
			}
		})
	}
}

func TestClassifyBuiltinToolResult_BashSandboxFailure(t *testing.T) {
	t.Parallel()
	raw := sandboxFailureJSON(sandbox.Result{
		ExitCode: 127,
		Stderr:   "not found",
	})
	tr := classifyBuiltinToolResult("c1", toolNameBash, raw, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if !tr.ExitCodeSet || tr.ExitCode != 127 {
		t.Fatalf("ExitCode = %d, ExitCodeSet = %v, want 127/true", tr.ExitCode, tr.ExitCodeSet)
	}
}

func TestClassifyPrecomputedReadResult_ErrorShapedFileContent(t *testing.T) {
	t.Parallel()
	raw := `{"error":"cached api failure"}`
	tr := classifyPrecomputedReadResult("c1", raw, 10)
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("Status = %s, want success for error-shaped file content", tr.Status)
	}
	if tr.Content != raw {
		t.Fatalf("Content = %q, want raw preserved", tr.Content)
	}
	if strings.Contains(tr.ForContext(), "[ERROR]") {
		t.Fatalf("ForContext() = %q, want unprefixed file content", tr.ForContext())
	}
}

func TestClassifyPrecomputedReadResult_PrefixedToolError(t *testing.T) {
	t.Parallel()
	raw := toolErrorPrefix + `{"error":"stat failed: no such file"}`
	tr := classifyPrecomputedReadResult("c1", raw, 10)
	if tr.Status != ToolStatusError {
		t.Fatalf("Status = %s, want error", tr.Status)
	}
	if tr.Error == nil || tr.Error.Message != "stat failed: no such file" {
		t.Fatalf("Error.Message = %v, want stat failed", tr.Error)
	}
}

func TestClassifyPrecomputedToolResult_BashFatalEnvelope(t *testing.T) {
	t.Parallel()
	tr := classifyPrecomputedToolResult("c1", toolNameBash, `{"FatalError":"sandbox crash"}`, 10)
	if tr.Status != ToolStatusFatal {
		t.Fatalf("Status = %s, want fatal", tr.Status)
	}
}

func TestClassifyCapabilityRawResult_ArbitraryJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{"multi_key_error", `{"error":"no matches","items":[]}`},
		{"status_timeout", `{"status":"timeout"}`},
		{"success_false", `{"Success":false,"ExitCode":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := classifyCapabilityRawResult("c1", tc.raw, 10)
			if tr.Status != ToolStatusSuccess {
				t.Fatalf("Status = %s, want success", tr.Status)
			}
			if tr.Content != tc.raw {
				t.Fatalf("Content = %q, want raw preserved %q", tr.Content, tc.raw)
			}
		})
	}
}
