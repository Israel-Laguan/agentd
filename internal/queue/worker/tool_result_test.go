package worker

import (
	"strings"
	"testing"
)

func TestToolStatus_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status ToolStatus
		want   string
	}{
		{ToolStatusSuccess, "success"},
		{ToolStatusError, "error"},
		{ToolStatusTimeout, "timeout"},
		{ToolStatusVetoed, "vetoed"},
		{ToolStatusFatal, "fatal"},
		{ToolStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("ToolStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestForContext_Success(t *testing.T) {
	t.Parallel()
	tr := SuccessResult("c1", "file contents here", 50)
	if got := tr.ForContext(); got != "file contents here" {
		t.Fatalf("ForContext() = %q, want raw content", got)
	}
}

func TestForContext_Vetoed(t *testing.T) {
	t.Parallel()
	tr := VetoedResult("c1", "dangerous command blocked")
	got := tr.ForContext()
	want := "[POLICY] Tool call blocked: dangerous command blocked"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_Vetoed_DescribesPolicyNotTechnicalFailure(t *testing.T) {
	t.Parallel()
	tr := VetoedResult("c1", "rm -rf / not allowed")
	got := tr.ForContext()
	if got == "" {
		t.Fatal("ForContext() should not be empty")
	}
	if tr.Status != ToolStatusVetoed {
		t.Fatalf("Status = %s, want vetoed", tr.Status)
	}
	// Must describe a policy constraint, not a technical failure.
	if got != "[POLICY] Tool call blocked: rm -rf / not allowed" {
		t.Fatalf("ForContext() = %q, want policy prefix", got)
	}
}

func TestForContext_Timeout(t *testing.T) {
	t.Parallel()
	tr := TimeoutResult("c1", 5000)
	got := tr.ForContext()
	want := "[TIMEOUT] tool did not respond within 5000ms"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_TimeoutUsesMutatedContent(t *testing.T) {
	t.Parallel()
	tr := TimeoutResult("c1", 5000)
	tr.Content = "scrubbed timeout message"
	got := tr.ForContext()
	want := "[TIMEOUT] scrubbed timeout message"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q (should use Content, not ElapsedMs fallback)", got, want)
	}
}

func TestForContext_TimeoutEmptyContentFallback(t *testing.T) {
	t.Parallel()
	tr := ToolResult{CallID: "c1", Status: ToolStatusTimeout, ElapsedMs: 3000}
	got := tr.ForContext()
	want := "[TIMEOUT] Tool did not respond within 3000ms"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_RetryableError(t *testing.T) {
	t.Parallel()
	tr := ErrorResult("c1", "connection reset", "ECONNRESET", 100)
	got := tr.ForContext()
	want := "[RETRYABLE ERROR] connection reset"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_NonRetryableError(t *testing.T) {
	t.Parallel()
	tr := NonRetryableErrorResult("c1", "permission denied", "EPERM", 100)
	got := tr.ForContext()
	want := "[ERROR] permission denied"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_Fatal(t *testing.T) {
	t.Parallel()
	tr := FatalResult("c1", "sandbox crashed", 200)
	got := tr.ForContext()
	want := "[FATAL] Tool execution failed unrecoverably: sandbox crashed"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_FatalEmptyMessage(t *testing.T) {
	t.Parallel()
	tr := ToolResult{Status: ToolStatusFatal}
	got := tr.ForContext()
	want := "[FATAL] Tool execution failed unrecoverably"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestSuccessResult_Fields(t *testing.T) {
	t.Parallel()
	tr := SuccessResult("call-1", "output", 42)
	if tr.CallID != "call-1" {
		t.Errorf("CallID = %q, want call-1", tr.CallID)
	}
	if tr.Status != ToolStatusSuccess {
		t.Errorf("Status = %s, want success", tr.Status)
	}
	if tr.Content != "output" {
		t.Errorf("Content = %q, want output", tr.Content)
	}
	if tr.Error != nil {
		t.Errorf("Error = %v, want nil", tr.Error)
	}
	if tr.Retryable {
		t.Error("Retryable = true, want false")
	}
	if tr.ElapsedMs != 42 {
		t.Errorf("ElapsedMs = %d, want 42", tr.ElapsedMs)
	}
}

func TestErrorResult_Fields(t *testing.T) {
	t.Parallel()
	tr := ErrorResult("call-2", "oops", "E001", 10)
	if tr.Status != ToolStatusError {
		t.Errorf("Status = %s, want error", tr.Status)
	}
	if !tr.Retryable {
		t.Error("Retryable = false, want true")
	}
	if tr.Error == nil {
		t.Fatal("Error is nil")
	}
	if tr.Error.Message != "oops" {
		t.Errorf("Error.Message = %q, want oops", tr.Error.Message)
	}
	if tr.Error.Code != "E001" {
		t.Errorf("Error.Code = %q, want E001", tr.Error.Code)
	}
	if !tr.Error.Retryable {
		t.Error("Error.Retryable = false, want true")
	}
}

func TestTimeoutResult_Fields(t *testing.T) {
	t.Parallel()
	tr := TimeoutResult("call-3", 3000)
	if tr.Status != ToolStatusTimeout {
		t.Errorf("Status = %s, want timeout", tr.Status)
	}
	if !tr.Retryable {
		t.Error("Retryable = false, want true")
	}
	if tr.Error == nil || tr.Error.Code != "TIMEOUT" {
		t.Error("Error.Code should be TIMEOUT")
	}
}

func TestVetoedResult_Fields(t *testing.T) {
	t.Parallel()
	tr := VetoedResult("call-4", "blocked")
	if tr.Status != ToolStatusVetoed {
		t.Errorf("Status = %s, want vetoed", tr.Status)
	}
	if tr.Retryable {
		t.Error("Retryable = true, want false")
	}
	if tr.ElapsedMs != 0 {
		t.Errorf("ElapsedMs = %d, want 0", tr.ElapsedMs)
	}
}

func TestFatalResult_Fields(t *testing.T) {
	t.Parallel()
	tr := FatalResult("call-5", "crash", 500)
	if tr.Status != ToolStatusFatal {
		t.Errorf("Status = %s, want fatal", tr.Status)
	}
	if tr.Retryable {
		t.Error("Retryable = true, want false")
	}
	if tr.Error == nil || tr.Error.Code != "FATAL" {
		t.Error("Error.Code should be FATAL")
	}
}

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

func TestForContext_UsesContentNotErrorMessage(t *testing.T) {
	t.Parallel()
	tr := NonRetryableErrorResult("c1", "original secret", "", 10)
	tr.Content = "scrubbed content"
	got := tr.ForContext()
	want := "[ERROR] scrubbed content"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q (should use Content, not Error.Message)", got, want)
	}
}

func TestForContext_VetoedUsesContentNotErrorMessage(t *testing.T) {
	t.Parallel()
	tr := VetoedResult("c1", "original reason")
	tr.Content = "scrubbed reason"
	got := tr.ForContext()
	want := "[POLICY] Tool call blocked: scrubbed reason"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q (should use Content, not Error.Message)", got, want)
	}
}

func TestForContext_FatalUsesContentNotErrorMessage(t *testing.T) {
	t.Parallel()
	tr := FatalResult("c1", "original crash", 10)
	tr.Content = "scrubbed crash"
	got := tr.ForContext()
	want := "[FATAL] Tool execution failed unrecoverably: scrubbed crash"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q (should use Content, not Error.Message)", got, want)
	}
}
