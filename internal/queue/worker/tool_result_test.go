package worker

import (
	"testing"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/sandbox"
)

func TestToolStatus_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status agenttools.ToolStatus
		want   string
	}{
		{agenttools.ToolStatusSuccess, "success"},
		{agenttools.ToolStatusError, "error"},
		{agenttools.ToolStatusTimeout, "timeout"},
		{agenttools.ToolStatusVetoed, "vetoed"},
		{agenttools.ToolStatusFatal, "fatal"},
		{agenttools.ToolStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("ToolStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestForContext_Success(t *testing.T) {
	t.Parallel()
	tr := agenttools.SuccessResult("c1", "file contents here", 50)
	if got := tr.ForContext(); got != "file contents here" {
		t.Fatalf("ForContext() = %q, want raw content", got)
	}
}

func TestForContext_Vetoed(t *testing.T) {
	t.Parallel()
	tr := agenttools.VetoedResult("c1", "dangerous command blocked")
	got := tr.ForContext()
	want := "[POLICY] Tool call blocked: dangerous command blocked"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_Vetoed_DescribesPolicyNotTechnicalFailure(t *testing.T) {
	t.Parallel()
	tr := agenttools.VetoedResult("c1", "rm -rf / not allowed")
	got := tr.ForContext()
	if got == "" {
		t.Fatal("ForContext() should not be empty")
	}
	if tr.Status != agenttools.ToolStatusVetoed {
		t.Fatalf("Status = %s, want vetoed", tr.Status)
	}
	// Must describe a policy constraint, not a technical failure.
	if got != "[POLICY] Tool call blocked: rm -rf / not allowed" {
		t.Fatalf("ForContext() = %q, want policy prefix", got)
	}
}

func TestForContext_Timeout(t *testing.T) {
	t.Parallel()
	tr := agenttools.TimeoutResult("c1", 5000)
	got := tr.ForContext()
	want := "[TIMEOUT] tool did not respond within 5000ms"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_TimeoutUsesMutatedContent(t *testing.T) {
	t.Parallel()
	tr := agenttools.TimeoutResult("c1", 5000)
	tr.Content = "scrubbed timeout message"
	got := tr.ForContext()
	want := "[TIMEOUT] scrubbed timeout message"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q (should use Content, not ElapsedMs fallback)", got, want)
	}
}

func TestForContext_TimeoutEmptyContentFallback(t *testing.T) {
	t.Parallel()
	tr := agenttools.ToolResult{CallID: "c1", Status: agenttools.ToolStatusTimeout, ElapsedMs: 3000}
	got := tr.ForContext()
	want := "[TIMEOUT] Tool did not respond within 3000ms"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_RetryableError(t *testing.T) {
	t.Parallel()
	tr := agenttools.ErrorResult("c1", "connection reset", "ECONNRESET", 100)
	got := tr.ForContext()
	want := "[RETRYABLE ERROR] connection reset"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_NonRetryableError(t *testing.T) {
	t.Parallel()
	tr := agenttools.NonRetryableErrorResult("c1", "permission denied", "EPERM", 100)
	got := tr.ForContext()
	want := "[ERROR] permission denied"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_Fatal(t *testing.T) {
	t.Parallel()
	tr := agenttools.FatalResult("c1", "sandbox crashed", 200)
	got := tr.ForContext()
	want := "[FATAL] Tool execution failed unrecoverably: sandbox crashed"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestForContext_FatalEmptyMessage(t *testing.T) {
	t.Parallel()
	tr := agenttools.ToolResult{Status: agenttools.ToolStatusFatal}
	got := tr.ForContext()
	want := "[FATAL] Tool execution failed unrecoverably"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q", got, want)
	}
}

func TestToolResultExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tr   agenttools.ToolResult
		want int
	}{
		{"success", agenttools.SuccessResult("c1", "ok", 0), 0},
		{
			"bash_failure", agenttools.ClassifyBuiltinToolResult("c1", agenttools.ToolNameBash, agenttools.SandboxFailureJSON(sandbox.Result{ExitCode: 127}), 0), 127,
		},
		{
			"bash_failure_no_exit_code", agenttools.ClassifyBuiltinToolResult("c1", agenttools.ToolNameBash, agenttools.SandboxFailureJSON(sandbox.Result{}), 0), -1,
		},
		{"vetoed", agenttools.VetoedResult("c1", "blocked"), -1},
		{"error_no_code", agenttools.NonRetryableErrorResult("c1", "fail", "", 0), -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := toolResultExitCode(tt.tr); got != tt.want {
				t.Fatalf("toolResultExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSuccessResult_Fields(t *testing.T) {
	t.Parallel()
	tr := agenttools.SuccessResult("call-1", "output", 42)
	if tr.CallID != "call-1" {
		t.Errorf("CallID = %q, want call-1", tr.CallID)
	}
	if tr.Status != agenttools.ToolStatusSuccess {
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
	tr := agenttools.ErrorResult("call-2", "oops", "E001", 10)
	if tr.Status != agenttools.ToolStatusError {
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
	tr := agenttools.TimeoutResult("call-3", 3000)
	if tr.Status != agenttools.ToolStatusTimeout {
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
	tr := agenttools.VetoedResult("call-4", "blocked")
	if tr.Status != agenttools.ToolStatusVetoed {
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
	tr := agenttools.FatalResult("call-5", "crash", 500)
	if tr.Status != agenttools.ToolStatusFatal {
		t.Errorf("Status = %s, want fatal", tr.Status)
	}
	if tr.Retryable {
		t.Error("Retryable = true, want false")
	}
	if tr.Error == nil || tr.Error.Code != "FATAL" {
		t.Error("Error.Code should be FATAL")
	}
}

func TestForContext_UsesContentNotErrorMessage(t *testing.T) {
	t.Parallel()
	tr := agenttools.NonRetryableErrorResult("c1", "original secret", "", 10)
	tr.Content = "scrubbed content"
	got := tr.ForContext()
	want := "[ERROR] scrubbed content"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q (should use Content, not Error.Message)", got, want)
	}
}

func TestForContext_VetoedUsesContentNotErrorMessage(t *testing.T) {
	t.Parallel()
	tr := agenttools.VetoedResult("c1", "original reason")
	tr.Content = "scrubbed reason"
	got := tr.ForContext()
	want := "[POLICY] Tool call blocked: scrubbed reason"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q (should use Content, not Error.Message)", got, want)
	}
}

func TestForContext_FatalUsesContentNotErrorMessage(t *testing.T) {
	t.Parallel()
	tr := agenttools.FatalResult("c1", "original crash", 10)
	tr.Content = "scrubbed crash"
	got := tr.ForContext()
	want := "[FATAL] Tool execution failed unrecoverably: scrubbed crash"
	if got != want {
		t.Fatalf("ForContext() = %q, want %q (should use Content, not Error.Message)", got, want)
	}
}
