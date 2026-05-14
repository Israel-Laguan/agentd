package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// --- DryRunHook unit tests ---

func TestDryRunHook_Disabled_AllowsExecution(t *testing.T) {
	t.Parallel()
	hook := DryRunHook(false)
	ctx := HookContext{ToolName: "bash", Args: `{"command":"ls"}`, Timestamp: time.Now()}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("disabled dry-run should not veto")
	}
}

func TestDryRunHook_Enabled_VetoesBash(t *testing.T) {
	t.Parallel()
	hook := DryRunHook(true)
	ctx := HookContext{ToolName: "bash", Args: `{"command":"echo hi"}`, Timestamp: time.Now()}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("dry-run should veto bash")
	}
	if verdict.Result != "(simulated) command executed successfully" {
		t.Fatalf("unexpected result: %q", verdict.Result)
	}
}

func TestDryRunHook_Enabled_VetoesRead(t *testing.T) {
	t.Parallel()
	hook := DryRunHook(true)
	ctx := HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("dry-run should veto read")
	}
	if verdict.Result != "(simulated) file contents" {
		t.Fatalf("unexpected result: %q", verdict.Result)
	}
}

func TestDryRunHook_Enabled_VetoesWrite(t *testing.T) {
	t.Parallel()
	hook := DryRunHook(true)
	ctx := HookContext{ToolName: "write", Args: `{"path":"a.txt","content":"x"}`, Timestamp: time.Now()}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("dry-run should veto write")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(verdict.Result), &parsed); err != nil {
		t.Fatalf("write result should be valid JSON: %v", err)
	}
	if parsed["success"] != true {
		t.Fatalf("expected success=true, got %v", parsed["success"])
	}
	if parsed["simulated"] != true {
		t.Fatalf("expected simulated=true, got %v", parsed["simulated"])
	}
}

func TestDryRunHook_Enabled_UnknownTool(t *testing.T) {
	t.Parallel()
	hook := DryRunHook(true)
	ctx := HookContext{ToolName: "custom_tool", Args: `{}`, Timestamp: time.Now()}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("dry-run should veto unknown tools")
	}
	if verdict.Result != "(simulated) tool executed successfully" {
		t.Fatalf("unexpected result for unknown tool: %q", verdict.Result)
	}
}

func TestDryRunHook_FailClosedPolicy(t *testing.T) {
	t.Parallel()
	hook := DryRunHook(true)
	if hook.Policy != FailClosed {
		t.Fatalf("expected FailClosed, got %d", hook.Policy)
	}
}

func TestDryRunHook_Name(t *testing.T) {
	t.Parallel()
	hook := DryRunHook(true)
	if hook.Name != "dry-run" {
		t.Fatalf("expected name 'dry-run', got %q", hook.Name)
	}
}

// --- Integration: dispatch-level tests ---

func TestDispatchTool_DryRun_SkipsExecution(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(DryRunHook(true))

	mockSB := &mockExecSandbox{result: sandbox.Result{
		Stdout: "REAL EXECUTION", Success: true,
	}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
	w := &Worker{toolExecutor: executor, hooks: hc}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`},
	}

	result := w.DispatchTool(context.Background(), "s1", call, nil, executor)
	if strings.Contains(result, "REAL EXECUTION") {
		t.Fatal("dry-run should not execute the real sandbox")
	}
	if !strings.Contains(result, "(simulated)") {
		t.Fatalf("expected simulated result, got %q", result)
	}
}

func TestDispatchTool_DryRun_PostHooksStillFire(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(DryRunHook(true))

	postHookRan := false
	hc.RegisterPost(PostHook{
		Name:   "observer",
		Policy: FailOpen,
		Fn: func(_ HookContext, result string) (string, error) {
			postHookRan = true
			return result + " [audited]", nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "nope", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
	w := &Worker{toolExecutor: executor, hooks: hc}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "read", Arguments: `{"path":"f.txt"}`},
	}

	result := w.DispatchTool(context.Background(), "s1", call, nil, executor)
	if !postHookRan {
		t.Fatal("post-hooks should fire during dry-run")
	}
	if !strings.Contains(result, "[audited]") {
		t.Fatalf("expected post-hook annotation, got %q", result)
	}
}

func TestDispatchTool_DryRun_Disabled_ExecutesNormally(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(DryRunHook(false))

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "real output\n", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
	w := &Worker{toolExecutor: executor, hooks: hc}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`},
	}

	result := w.DispatchTool(context.Background(), "s1", call, nil, executor)
	if strings.Contains(result, "(simulated)") {
		t.Fatal("disabled dry-run should execute normally")
	}
	if !strings.Contains(result, "real output") {
		t.Fatalf("expected real output, got %q", result)
	}
}

func TestDispatchTool_DryRun_AllToolTypes(t *testing.T) {
	t.Parallel()
	tools := []struct {
		name     string
		args     string
		contains string
	}{
		{"bash", `{"command":"ls"}`, "(simulated) command executed successfully"},
		{"read", `{"path":"a.txt"}`, "(simulated) file contents"},
		{"write", `{"path":"a.txt","content":"x"}`, `"success":true`},
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hc := NewHookChain()
			hc.RegisterPre(DryRunHook(true))

			mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "nope", Success: true}}
			executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
			w := &Worker{toolExecutor: executor, hooks: hc}

			call := gateway.ToolCall{
				ID:       "call_1",
				Function: gateway.ToolCallFunction{Name: tt.name, Arguments: tt.args},
			}

			result := w.DispatchTool(context.Background(), "s1", call, nil, executor)
			if !strings.Contains(result, tt.contains) {
				t.Fatalf("expected result to contain %q, got %q", tt.contains, result)
			}
		})
	}
}
