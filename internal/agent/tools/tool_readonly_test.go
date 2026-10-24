package tools

import (
	"context"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// capturingSandbox records the payload of each execution.
type capturingSandbox struct {
	payloads []sandbox.Payload
}

func (s *capturingSandbox) Execute(_ context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	s.payloads = append(s.payloads, payload)
	return sandbox.Result{Success: true}, nil
}

func runBashCapturing(t *testing.T, ex *ToolExecutor, sb *capturingSandbox) sandbox.Payload {
	t.Helper()
	call := gateway.ToolCall{ID: "c1", Type: "function", Function: gateway.ToolCallFunction{Name: ToolNameBash, Arguments: `{"command":"ls"}`}}
	ex.Execute(context.Background(), call)
	if len(sb.payloads) != 1 {
		t.Fatalf("sandbox ran %d times, want 1", len(sb.payloads))
	}
	return sb.payloads[0]
}

// A read-only workspace reaches the sandbox on every bash payload, so the jail
// mounts it read-only.
func TestToolExecutorReadOnlyWorkspaceReachesSandbox(t *testing.T) {
	t.Parallel()
	sb := &capturingSandbox{}
	ex := NewToolExecutor(sb, t.TempDir(), nil, time.Second).WithReadOnlyWorkspace(true)

	if got := runBashCapturing(t, ex, sb); !got.ReadOnlyWorkspace {
		t.Fatal("bash payload ReadOnlyWorkspace = false, want true for a read-only executor")
	}
}

// The default executor stays writable, so existing profiles are unchanged.
func TestToolExecutorDefaultWorkspaceIsWritable(t *testing.T) {
	t.Parallel()
	sb := &capturingSandbox{}
	ex := NewToolExecutor(sb, t.TempDir(), nil, time.Second)

	if got := runBashCapturing(t, ex, sb); got.ReadOnlyWorkspace {
		t.Fatal("bash payload ReadOnlyWorkspace = true for an executor that was never made read-only")
	}
	if ex.ReadOnlyWorkspace() {
		t.Fatal("ReadOnlyWorkspace() = true on a default executor")
	}
}
