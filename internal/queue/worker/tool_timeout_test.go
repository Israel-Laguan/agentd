package worker

import (
	"context"
	"encoding/json"
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

	result := w.DispatchTool(context.Background(), "s1", call, nil, executor)

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if parsed["status"] != "timeout" {
		t.Fatalf("status = %q, want \"timeout\"", parsed["status"])
	}
	if !strings.HasPrefix(parsed["error"], "[TIMEOUT]") {
		t.Fatalf("error should start with [TIMEOUT], got %q", parsed["error"])
	}
	if !strings.Contains(parsed["error"], "bash") {
		t.Fatalf("error should mention tool name, got %q", parsed["error"])
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
	result := w.DispatchTool(context.Background(), "s1", call, nil, executor)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("per-tool timeout (50ms) should have fired, but elapsed %v", elapsed)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if parsed["status"] != "timeout" {
		t.Fatalf("status = %q, want \"timeout\"", parsed["status"])
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

	result := w.DispatchTool(context.Background(), "s1", call, nil, executor)

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if parsed["status"] != "timeout" {
		t.Fatalf("default timeout should have fired; status = %q", parsed["status"])
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

	result := w.DispatchTool(context.Background(), "s1", call, nil, executor)
	if strings.Contains(result, "[TIMEOUT]") {
		t.Fatalf("fast tool should not timeout, got %q", result)
	}
	if !strings.Contains(result, "hello") {
		t.Fatalf("expected output 'hello', got %q", result)
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

	timeoutResult := w.DispatchTool(context.Background(), "s1", timeoutCall, nil, executor)

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
	errorResult := w2.DispatchTool(context.Background(), "s1", errorCall, nil, errExecutor)

	var tRes map[string]string
	if err := json.Unmarshal([]byte(timeoutResult), &tRes); err != nil {
		t.Fatalf("timeout result not valid JSON: %v", err)
	}

	var eRes map[string]string
	if err := json.Unmarshal([]byte(errorResult), &eRes); err != nil {
		t.Fatalf("error result not valid JSON: %v", err)
	}

	if tRes["status"] != "timeout" {
		t.Fatalf("timeout result should have status=timeout, got %q", tRes["status"])
	}
	if eRes["status"] == "timeout" {
		t.Fatal("error result should NOT have status=timeout")
	}
	if !strings.HasPrefix(tRes["error"], "[TIMEOUT]") {
		t.Fatalf("timeout error should start with [TIMEOUT], got %q", tRes["error"])
	}
	if strings.HasPrefix(eRes["error"], "[TIMEOUT]") {
		t.Fatal("error result should NOT start with [TIMEOUT]")
	}
}
