package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// slowSandbox blocks until the context is cancelled, simulating a tool
// that exceeds its timeout.
type slowSandbox struct {
	delay time.Duration
}

func (s *slowSandbox) Execute(ctx context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	select {
	case <-time.After(s.delay):
		return sandbox.Result{Stdout: "done", Success: true}, nil
	case <-ctx.Done():
		return sandbox.Result{}, ctx.Err()
	}
}

func TestDispatchTool_Timeout_ReturnsTypedResult(t *testing.T) {
	t.Parallel()
	sb := &slowSandbox{delay: 5 * time.Second}
	executor := NewToolExecutor(sb, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		toolTimeouts: config.ToolTimeoutsConfig{
			Defaults: map[string]time.Duration{
				"bash": 50 * time.Millisecond,
			},
		},
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 10"}`},
	}

	tr := w.DispatchTool(context.Background(), "s1", call, nil, executor)

	if tr.Status != ToolStatusTimeout {
		t.Fatalf("status = %s, want timeout", tr.Status)
	}
	forCtx := tr.ForContext()
	if !strings.HasPrefix(forCtx, "[TIMEOUT]") {
		t.Fatalf("ForContext should start with [TIMEOUT], got %q", forCtx)
	}
}

func TestDispatchTool_Timeout_PerToolOverridesDefault(t *testing.T) {
	t.Parallel()
	sb := &slowSandbox{delay: 5 * time.Second}
	executor := NewToolExecutor(sb, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		toolTimeouts: config.ToolTimeoutsConfig{
			Defaults: map[string]time.Duration{
				"bash":    50 * time.Millisecond,
				"default": 10 * time.Second,
			},
		},
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 10"}`},
	}

	start := time.Now()
	tr := w.DispatchTool(context.Background(), "s1", call, nil, executor)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("per-tool timeout (50ms) should have fired, but elapsed %v", elapsed)
	}

	if tr.Status != ToolStatusTimeout {
		t.Fatalf("status = %s, want timeout", tr.Status)
	}
}

func TestDispatchTool_Timeout_DefaultApplies(t *testing.T) {
	t.Parallel()
	sb := &slowSandbox{delay: 5 * time.Second}
	executor := NewToolExecutor(sb, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		toolTimeouts: config.ToolTimeoutsConfig{
			Defaults: map[string]time.Duration{
				"default": 50 * time.Millisecond,
			},
		},
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 10"}`},
	}

	tr := w.DispatchTool(context.Background(), "s1", call, nil, executor)

	if tr.Status != ToolStatusTimeout {
		t.Fatalf("default timeout should have fired; status = %s", tr.Status)
	}
}

func TestDispatchTool_NoTimeout_FastTool(t *testing.T) {
	t.Parallel()
	sb := &mockExecSandbox{result: sandbox.Result{Stdout: "hello\n", Success: true}}
	executor := NewToolExecutor(sb, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		toolTimeouts: config.ToolTimeoutsConfig{
			Defaults: map[string]time.Duration{
				"bash":    10 * time.Second,
				"default": 30 * time.Second,
			},
		},
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`},
	}

	tr := w.DispatchTool(context.Background(), "s1", call, nil, executor)
	if tr.Status == ToolStatusTimeout {
		t.Fatalf("fast tool should not timeout, got status %s", tr.Status)
	}
	if !strings.Contains(tr.Content, "hello") {
		t.Fatalf("expected output 'hello', got %q", tr.Content)
	}
}

// TestDispatchToolWithHooks_Timeout verifies that the production agentic path
// (dispatchToolWithHooks → dispatchToolWithProject) also enforces per-tool
// timeouts, not just the public DispatchTool entry point.
func TestDispatchToolWithHooks_Timeout(t *testing.T) {
	t.Parallel()
	sb := &slowSandbox{delay: 5 * time.Second}
	executor := NewToolExecutor(sb, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		toolTimeouts: config.ToolTimeoutsConfig{
			Defaults: map[string]time.Duration{
				"bash": 50 * time.Millisecond,
			},
		},
	}

	call := gateway.ToolCall{
		ID:       "call_hooks",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 10"}`},
	}

	tr, suspend := w.dispatchToolWithHooks(
		context.Background(), "s1", "p1", time.Now(),
		call, nil, executor, nil, nil,
	)

	if suspend {
		t.Fatal("expected suspend=false")
	}

	if tr.Status != ToolStatusTimeout {
		t.Fatalf("status = %s, want timeout", tr.Status)
	}
	forCtx := tr.ForContext()
	if !strings.HasPrefix(forCtx, "[TIMEOUT]") {
		t.Fatalf("ForContext should start with [TIMEOUT], got %q", forCtx)
	}
}

func TestDispatchTool_Timeout_DistinguishableFromError(t *testing.T) {
	t.Parallel()
	sb := &slowSandbox{delay: 5 * time.Second}
	executor := NewToolExecutor(sb, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		toolTimeouts: config.ToolTimeoutsConfig{
			Defaults: map[string]time.Duration{
				"bash": 50 * time.Millisecond,
			},
		},
	}

	timeoutCall := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 10"}`},
	}

	timeoutTR := w.DispatchTool(context.Background(), "s1", timeoutCall, nil, executor)

	errSB := &mockExecSandbox{result: sandbox.Result{
		Success: false, ExitCode: 1,
		Stdout: "", Stderr: "command not found",
	}}
	errExecutor := NewToolExecutor(errSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
	w2 := &Worker{
		toolExecutor: errExecutor,
		toolTimeouts: config.ToolTimeoutsConfig{
			Defaults: map[string]time.Duration{"bash": 10 * time.Second},
		},
	}
	errorCall := gateway.ToolCall{
		ID:       "call_2",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"bad"}`},
	}
	errorTR := w2.DispatchTool(context.Background(), "s1", errorCall, nil, errExecutor)

	if timeoutTR.Status != ToolStatusTimeout {
		t.Fatalf("timeout result should have status=timeout, got %s", timeoutTR.Status)
	}
	if errorTR.Status == ToolStatusTimeout {
		t.Fatal("error result should NOT have status=timeout")
	}
	timeoutCtx := timeoutTR.ForContext()
	if !strings.HasPrefix(timeoutCtx, "[TIMEOUT]") {
		t.Fatalf("timeout ForContext should start with [TIMEOUT], got %q", timeoutCtx)
	}
	errorCtx := errorTR.ForContext()
	if strings.HasPrefix(errorCtx, "[TIMEOUT]") {
		t.Fatal("error ForContext should NOT start with [TIMEOUT]")
	}
}
