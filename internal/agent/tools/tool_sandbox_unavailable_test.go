package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type refusingSandbox struct{ err error }

func (s refusingSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	return sandbox.Result{ExitCode: -1}, s.err
}

func runBashThrough(t *testing.T, err error) ToolResult {
	t.Helper()
	ex := NewToolExecutor(refusingSandbox{err: err}, t.TempDir(), nil, time.Second)
	call := gateway.ToolCall{ID: "c1", Type: "function", Function: gateway.ToolCallFunction{Name: ToolNameBash, Arguments: `{"command":"uname -a"}`}}
	return ClassifyBuiltinToolResult("c1", ToolNameBash, ex.Execute(context.Background(), call), 0)
}

// B-052: a required jail that is unavailable is unrecoverable for the model, so
// the bash tool reports it as fatal and tagged, which ends the agentic loop.
func TestBashSandboxUnavailableIsFatalAndTagged(t *testing.T) {
	t.Parallel()
	tr := runBashThrough(t, fmt.Errorf("%w: sandbox.backend=bubblewrap is required, but bwrap not found; command refused", models.ErrSandboxUnavailable))
	if tr.Status != ToolStatusFatal || tr.Retryable {
		t.Fatalf("status = %s retryable = %v, want fatal and not retryable", tr.Status, tr.Retryable)
	}
	if tr.Error == nil || tr.Error.Code != ToolErrorCodeSandboxUnavailable {
		t.Fatalf("error = %#v, want code %q", tr.Error, ToolErrorCodeSandboxUnavailable)
	}
	if !strings.Contains(tr.Content, "bwrap not found") {
		t.Fatalf("content = %q, want the refusal reason", tr.Content)
	}
}

// Other execution errors keep today's non-fatal shape so the model can react.
func TestBashOtherExecutionErrorStaysNonFatal(t *testing.T) {
	t.Parallel()
	tr := runBashThrough(t, errors.New("start command: boom"))
	if tr.Status == ToolStatusFatal {
		t.Fatalf("status = %s, want a non-fatal error", tr.Status)
	}
	if tr.Error != nil && tr.Error.Code == ToolErrorCodeSandboxUnavailable {
		t.Fatalf("error code = %q, want untagged", tr.Error.Code)
	}
}

// B-052 follow-up: a delegate result tagged SANDBOX_UNAVAILABLE classifies as
// fatal with the code, alone or inside a parallel batch behind another failure.
func TestClassifyDelegateRawResultCarriesSandboxUnavailable(t *testing.T) {
	t.Parallel()
	tagged := `{"status":"failure","error":"refused","error_code":"SANDBOX_UNAVAILABLE"}`
	other := `{"status":"failure","error":"boom"}`
	for name, raw := range map[string]string{
		"single":        tagged,
		"batch":         "[" + other + "," + tagged + "]",
		"batch-success": `[{"status":"success"},` + tagged + "]",
	} {
		tr := ClassifyDelegateRawResult("c1", raw, 1)
		if tr.Status != ToolStatusFatal || tr.Error == nil || tr.Error.Code != ToolErrorCodeSandboxUnavailable {
			t.Fatalf("%s: status=%s error=%+v, want fatal SANDBOX_UNAVAILABLE", name, tr.Status, tr.Error)
		}
	}
	if tr := ClassifyDelegateRawResult("c1", other, 1); tr.Status == ToolStatusFatal {
		t.Fatalf("untagged failure must stay non-fatal, got %s", tr.Status)
	}
}
