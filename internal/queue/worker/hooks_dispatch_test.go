package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

func TestDispatchTool_PreHookVeto(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(PreHook{
		Name:   "deny-all",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{Veto: true, Reason: "denied"}, nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "should not run", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`},
	}

	result := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if !strings.Contains(result, "vetoed") {
		t.Fatalf("expected vetoed result, got %q", result)
	}
	if strings.Contains(result, "should not run") {
		t.Fatal("tool should not have executed after veto")
	}
}

func TestDispatchTool_PostHookMutation(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{
		Name:   "annotate",
		Policy: FailOpen,
		Fn: func(_ HookContext, result string) (string, error) {
			return result + " [hooked]", nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "hello\n", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`},
	}

	result := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if !strings.Contains(result, "[hooked]") {
		t.Fatalf("expected post-hook annotation, got %q", result)
	}
}

func TestDispatchTool_NilHooksNoChange(t *testing.T) {
	t.Parallel()
	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "hello\n", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        nil,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`},
	}

	result := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if !strings.Contains(result, "hello") {
		t.Fatalf("expected result to contain 'hello', got %q", result)
	}
}

func TestDispatchTool_SessionIDPropagated(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	var captured string
	hc.RegisterPre(PreHook{
		Name: "capture", Policy: FailOpen,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			captured = ctx.SessionID
			return HookVerdict{}, nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "ok\n", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
	w := &Worker{toolExecutor: executor, hooks: hc}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo ok"}`},
	}

	w.DispatchTool(context.Background(), "task-42", call, nil, executor)
	if captured != "task-42" {
		t.Fatalf("expected SessionID 'task-42', got %q", captured)
	}
}

func TestDispatchTool_EmptyHooksNoChange(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "hello\n", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`},
	}

	result := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if !strings.Contains(result, "hello") {
		t.Fatalf("expected result to contain 'hello', got %q", result)
	}
}

func TestHookChain_NilFn_PreHook_FailOpen(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(PreHook{Name: "nil-hook", Policy: FailOpen, Fn: nil})

	verdict := hc.RunPre(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if verdict.Veto {
		t.Fatal("nil Fn with FailOpen should not veto")
	}
}

func TestHookChain_NilFn_PreHook_FailClosed(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(PreHook{Name: "nil-hook", Policy: FailClosed, Fn: nil})

	verdict := hc.RunPre(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if !verdict.Veto {
		t.Fatal("nil Fn with FailClosed should veto")
	}
	if !strings.Contains(verdict.Reason, "fail_closed") {
		t.Fatalf("expected fail_closed in reason, got %q", verdict.Reason)
	}
}

func TestHookChain_NilFn_PostHook_FailOpen(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{Name: "nil-hook", Policy: FailOpen, Fn: nil})

	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "original")
	if got != "original" {
		t.Fatalf("nil Fn with FailOpen should preserve result, got %q", got)
	}
}

func TestHookChain_NilFn_PostHook_FailClosed(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{Name: "nil-hook", Policy: FailClosed, Fn: nil})

	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "original")
	if !strings.Contains(got, "fail_closed") {
		t.Fatalf("nil Fn with FailClosed should return error, got %q", got)
	}
}

func TestHookChain_NilFn_SessionStart_FailOpen(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	secondRan := false
	hc.RegisterSessionStart(SessionStartHook{Name: "nil-hook", Policy: FailOpen, Fn: nil})
	hc.RegisterSessionStart(SessionStartHook{
		Name: "second", Policy: FailOpen,
		Fn: func(HookContext) error { secondRan = true; return nil },
	})

	err := hc.RunSessionStart(HookContext{SessionID: "s1", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("nil Fn with FailOpen should not error: %v", err)
	}
	if !secondRan {
		t.Fatal("second hook should still run after nil Fn with FailOpen")
	}
}

func TestHookChain_NilFn_SessionStart_FailClosed(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterSessionStart(SessionStartHook{Name: "nil-hook", Policy: FailClosed, Fn: nil})

	err := hc.RunSessionStart(HookContext{SessionID: "s1", Timestamp: time.Now()})
	if err == nil {
		t.Fatal("nil Fn with FailClosed should return error")
	}
}
