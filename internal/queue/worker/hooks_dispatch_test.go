package worker

import (
	"context"
	"strings"
	"testing"

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

	result := w.DispatchTool(context.Background(), call, nil, executor)
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

	result := w.DispatchTool(context.Background(), call, nil, executor)
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

	result := w.DispatchTool(context.Background(), call, nil, executor)
	if !strings.Contains(result, "hello") {
		t.Fatalf("expected result to contain 'hello', got %q", result)
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

	result := w.DispatchTool(context.Background(), call, nil, executor)
	if !strings.Contains(result, "hello") {
		t.Fatalf("expected result to contain 'hello', got %q", result)
	}
}
