package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func TestDispatchTool_PreHookVeto(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.PreHook{
		Name:   "deny-all",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, Reason: "denied"}, nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "should not run", Success: true}}
	executor := agenttools.NewToolExecutor(mockSB, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`},
	}

	tr := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if tr.Status != agenttools.ToolStatusVetoed {
		t.Fatalf("expected vetoed status, got %s", tr.Status)
	}
	if strings.Contains(tr.Content, "should not run") {
		t.Fatal("tool should not have executed after veto")
	}
}

func TestDispatchTool_PostHookMutation(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	hc.RegisterPost(agenthooks.PostHook{
		Name:   "annotate",
		Policy: agenthooks.FailOpen,
		Fn: func(_ agenthooks.HookContext, result string) (string, error) {
			return result + " [hooked]", nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "hello\n", Success: true}}
	executor := agenttools.NewToolExecutor(mockSB, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`},
	}

	tr := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if !strings.Contains(tr.Content, "[hooked]") {
		t.Fatalf("expected post-hook annotation, got %q", tr.Content)
	}
}

func TestDispatchTool_NilHooksNoChange(t *testing.T) {
	t.Parallel()
	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "hello\n", Success: true}}
	executor := agenttools.NewToolExecutor(mockSB, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        nil,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`},
	}

	tr := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if !strings.Contains(tr.Content, "hello") {
		t.Fatalf("expected result to contain 'hello', got %q", tr.Content)
	}
}

func TestDispatchTool_SessionIDPropagated(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	var captured string
	hc.RegisterPre(agenthooks.PreHook{
		Name: "capture", Policy: agenthooks.FailOpen,
		Fn: func(ctx agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			captured = ctx.SessionID
			return agenthooks.HookVerdict{}, nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "ok\n", Success: true}}
	executor := agenttools.NewToolExecutor(mockSB, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)
	w := &Worker{toolExecutor: executor, hooks: hc}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo ok"}`},
	}

	_ = w.DispatchTool(context.Background(), "task-42", call, nil, executor)
	if captured != "task-42" {
		t.Fatalf("expected SessionID 'task-42', got %q", captured)
	}
}

func TestDispatchTool_EmptyHooksNoChange(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "hello\n", Success: true}}
	executor := agenttools.NewToolExecutor(mockSB, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`},
	}

	tr := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if !strings.Contains(tr.Content, "hello") {
		t.Fatalf("expected result to contain 'hello', got %q", tr.Content)
	}
}

func TestHookChain_NilFn_PreHook_FailOpen(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.PreHook{Name: "nil-hook", Policy: agenthooks.FailOpen, Fn: nil})

	verdict := hc.RunPre(agenthooks.HookContext{ToolName: "bash", Timestamp: time.Now()})
	if verdict.Veto {
		t.Fatal("nil Fn with FailOpen should not veto")
	}
}

func TestHookChain_NilFn_PreHook_FailClosed(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.PreHook{Name: "nil-hook", Policy: agenthooks.FailClosed, Fn: nil})

	verdict := hc.RunPre(agenthooks.HookContext{ToolName: "bash", Timestamp: time.Now()})
	if !verdict.Veto {
		t.Fatal("nil Fn with FailClosed should veto")
	}
	if !strings.Contains(verdict.Reason, "fail_closed") {
		t.Fatalf("expected fail_closed in reason, got %q", verdict.Reason)
	}
}

func TestHookChain_NilFn_PostHook_FailOpen(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	hc.RegisterPost(agenthooks.PostHook{Name: "nil-hook", Policy: agenthooks.FailOpen, Fn: nil})

	got := hc.RunPost(agenthooks.HookContext{ToolName: "bash", Timestamp: time.Now()}, "original")
	if got != "original" {
		t.Fatalf("nil Fn with FailOpen should preserve result, got %q", got)
	}
}

func TestHookChain_NilFn_PostHook_FailClosed(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	hc.RegisterPost(agenthooks.PostHook{Name: "nil-hook", Policy: agenthooks.FailClosed, Fn: nil})

	got := hc.RunPost(agenthooks.HookContext{ToolName: "bash", Timestamp: time.Now()}, "original")
	if !strings.Contains(got, "fail_closed") {
		t.Fatalf("nil Fn with FailClosed should return error, got %q", got)
	}
}

func TestHookChain_NilFn_SessionStart_FailOpen(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	secondRan := false
	hc.RegisterSessionStart(agenthooks.SessionStartHook{Name: "nil-hook", Policy: agenthooks.FailOpen, Fn: nil})
	hc.RegisterSessionStart(agenthooks.SessionStartHook{
		Name: "second", Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) error { secondRan = true; return nil },
	})

	err := hc.RunSessionStart(agenthooks.HookContext{SessionID: "s1", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("nil Fn with FailOpen should not error: %v", err)
	}
	if !secondRan {
		t.Fatal("second hook should still run after nil Fn with FailOpen")
	}
}

func TestHookChain_NilFn_SessionStart_FailClosed(t *testing.T) {
	t.Parallel()
	hc := agenthooks.NewHookChain()
	hc.RegisterSessionStart(agenthooks.SessionStartHook{Name: "nil-hook", Policy: agenthooks.FailClosed, Fn: nil})

	err := hc.RunSessionStart(agenthooks.HookContext{SessionID: "s1", Timestamp: time.Now()})
	if err == nil {
		t.Fatal("nil Fn with FailClosed should return error")
	}
}

func TestDispatchTool_VetoedRunsAuditHook(t *testing.T) {
	t.Parallel()

	sink := &mockEventSink{}
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.PreHook{
		Name:   "deny",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, Reason: "policy blocked"}, nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "never run", Success: true}}
	executor := agenttools.NewToolExecutor(mockSB, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

	w := NewWorker(&mockAgenticStore{}, nil, mockSB, nil, sink, WorkerOptions{Hooks: hc})

	call := gateway.ToolCall{
		ID:       "call_veto",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`},
	}

	tr := w.DispatchTool(context.Background(), "task-veto", call, nil, executor)
	if tr.Status != agenttools.ToolStatusVetoed {
		t.Fatalf("expected vetoed status, got %s", tr.Status)
	}
	if !strings.Contains(tr.Content, "policy blocked") {
		t.Fatalf("content = %q, want veto reason", tr.Content)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(sink.events))
	}
	if sink.events[0].Type != models.EventTypeToolCall || sink.events[1].Type != models.EventTypeToolResult {
		t.Fatalf("event types = %q, %q", sink.events[0].Type, sink.events[1].Type)
	}

	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1 for vetoed call", resultEvent.ExitCode)
	}
}

func TestDispatchToolWithHooks_VetoedRunsAuditHook(t *testing.T) {
	t.Parallel()

	sink := &mockEventSink{}
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "deny",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, Reason: "task policy blocked"}, nil
		},
	})

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "never run", Success: true}}
	executor := agenttools.NewToolExecutor(mockSB, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

	w := NewWorker(&mockAgenticStore{}, nil, mockSB, nil, sink, WorkerOptions{})

	call := gateway.ToolCall{
		ID:       "call_task_veto",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`},
	}

	tr, suspended := w.dispatchToolWithHooks(
		context.Background(),
		"task-veto-hooks",
		"proj-veto-hooks",
		"",
		time.Now(),
		call,
		nil,
		executor,
		taskHooks,
		nil,
	)
	if tr.Status != agenttools.ToolStatusVetoed {
		t.Fatalf("expected vetoed status, got %s", tr.Status)
	}
	if suspended {
		t.Fatal("expected non-suspend veto")
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(sink.events))
	}

	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1 for vetoed call", resultEvent.ExitCode)
	}
	if !strings.Contains(resultEvent.OutputSummary, "task policy blocked") {
		t.Fatalf("output summary = %q, want veto reason", resultEvent.OutputSummary)
	}
}
