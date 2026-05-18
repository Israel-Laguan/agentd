package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// --- Integration: ScrubResultHook + AuditHook via NewWorker ---

func TestNewWorker_RegistersScrubAndAuditHooks(t *testing.T) {
	t.Parallel()

	sink := &mockEventSink{}
	mockSB := &mockExecSandbox{result: sandbox.Result{
		Stdout:  "data with sk-AAAAAAAAAAAAAAAAAAAAAA leak\n",
		Success: true,
	}}

	w := NewWorker(
		&mockAgenticStore{},
		nil,
		mockSB,
		nil,
		sink,
		WorkerOptions{MaxToolIterations: 5},
	)

	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
	call := gateway.ToolCall{
		ID:       "call_integration",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"cat secret"}`},
	}

	tr := w.dispatchToolWithProject(
		context.Background(), "task-int", "proj-int", call, nil, executor, nil, false,
	)

	// Result should be scrubbed (ScrubResultHook runs first)
	if strings.Contains(tr.Content, "sk-AAAA") {
		t.Fatalf("result entering model context should be scrubbed: %q", tr.Content)
	}

	// Audit events should be emitted (AuditHook runs second)
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(sink.events))
	}
	if sink.events[0].Type != models.EventTypeToolCall {
		t.Fatalf("first event should be TOOL_CALL, got %q", sink.events[0].Type)
	}
	if sink.events[1].Type != models.EventTypeToolResult {
		t.Fatalf("second event should be TOOL_RESULT, got %q", sink.events[1].Type)
	}

	// Verify project/task IDs propagated
	if sink.events[0].ProjectID != "proj-int" {
		t.Fatalf("expected ProjectID 'proj-int', got %q", sink.events[0].ProjectID)
	}
}

func TestAuditHook_DispatchToolEmitsConsistently(t *testing.T) {
	t.Parallel()

	sink := &mockEventSink{}
	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "ok\n", Success: true}}

	w := NewWorker(
		&mockAgenticStore{},
		nil,
		mockSB,
		nil,
		sink,
		WorkerOptions{MaxToolIterations: 5},
	)

	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	// Call through DispatchTool (no projectID)
	call1 := gateway.ToolCall{
		ID:       "call_a",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo a"}`},
	}
	_ = w.DispatchTool(context.Background(), "task-a", call1, nil, executor)

	// Call through dispatchToolWithProject
	call2 := gateway.ToolCall{
		ID:       "call_b",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo b"}`},
	}
	_ = w.dispatchToolWithProject(context.Background(), "task-b", "proj-b", call2, nil, executor, nil, false)

	// Both should emit TOOL_CALL + TOOL_RESULT = 4 events total
	if len(sink.events) != 4 {
		t.Fatalf("expected 4 events from 2 dispatches, got %d", len(sink.events))
	}

	// Verify call IDs are correct
	var ev0, ev2 ToolCallEvent
	_ = json.Unmarshal([]byte(sink.events[0].Payload), &ev0)
	_ = json.Unmarshal([]byte(sink.events[2].Payload), &ev2)
	if ev0.CallID != "call_a" {
		t.Fatalf("first dispatch call_id: want 'call_a', got %q", ev0.CallID)
	}
	if ev2.CallID != "call_b" {
		t.Fatalf("second dispatch call_id: want 'call_b', got %q", ev2.CallID)
	}
}

// --- Clone / PrependPost tests ---

func TestHookChainClone_DoesNotMutateOriginal(t *testing.T) {
	t.Parallel()
	original := NewHookChain()
	original.RegisterPost(PostHook{
		Name: "existing", Policy: FailOpen,
		Fn: func(_ HookContext, r string) (string, error) { return r, nil },
	})

	clone := original.Clone()
	clone.RegisterPost(PostHook{
		Name: "added", Policy: FailOpen,
		Fn: func(_ HookContext, r string) (string, error) { return r + " cloned", nil },
	})

	// Original should still have only 1 post-hook
	original.mu.RLock()
	origLen := len(original.postHooks)
	original.mu.RUnlock()
	if origLen != 1 {
		t.Fatalf("original should have 1 post-hook, got %d", origLen)
	}

	clone.mu.RLock()
	cloneLen := len(clone.postHooks)
	clone.mu.RUnlock()
	if cloneLen != 2 {
		t.Fatalf("clone should have 2 post-hooks, got %d", cloneLen)
	}
}

func TestHookChainPrependPost_RunsBeforeExisting(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{
		Name: "append", Policy: FailOpen,
		Fn: func(_ HookContext, r string) (string, error) { return r + ":second", nil },
	})
	hc.PrependPost(PostHook{
		Name: "prepend", Policy: FailOpen,
		Fn: func(_ HookContext, r string) (string, error) { return r + ":first", nil },
	})

	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "start")
	if got != "start:first:second" {
		t.Fatalf("expected 'start:first:second', got %q", got)
	}
}

func TestNewWorker_SharedHookChainNotMutated(t *testing.T) {
	t.Parallel()
	shared := NewHookChain()
	shared.RegisterPost(PostHook{
		Name: "user-hook", Policy: FailOpen,
		Fn: func(_ HookContext, r string) (string, error) { return r, nil },
	})

	shared.mu.RLock()
	beforeLen := len(shared.postHooks)
	shared.mu.RUnlock()

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "ok\n", Success: true}}
	_ = NewWorker(&mockAgenticStore{}, nil, mockSB, nil, nil, WorkerOptions{
		MaxToolIterations: 5,
		Hooks:             shared,
	})

	shared.mu.RLock()
	afterLen := len(shared.postHooks)
	shared.mu.RUnlock()

	if afterLen != beforeLen {
		t.Fatalf("shared HookChain mutated: had %d post-hooks, now %d", beforeLen, afterLen)
	}
}

// --- Error path scrubbing ---

func TestErrorPathsRunThroughPostHooks(t *testing.T) {
	t.Parallel()

	sink := &mockEventSink{}
	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "ok\n", Success: true}}

	w := NewWorker(
		&mockAgenticStore{},
		nil,
		mockSB,
		nil,
		sink,
		WorkerOptions{MaxToolIterations: 5},
	)

	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	// Call an unknown tool — this used to bypass RunPost
	call := gateway.ToolCall{
		ID:       "call_unknown",
		Function: gateway.ToolCallFunction{Name: "nonexistent", Arguments: `{}`},
	}
	tr := w.dispatchToolWithProject(context.Background(), "task-err", "proj-err", call, nil, executor, nil, false)

	// Result should contain error message
	if !strings.Contains(tr.Content, "unknown tool") {
		t.Fatalf("expected unknown tool error, got %q", tr.Content)
	}

	// Audit events should still fire (error path goes through RunPost)
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events for error path, got %d", len(sink.events))
	}
	if sink.events[0].Type != models.EventTypeToolCall {
		t.Fatalf("expected TOOL_CALL, got %q", sink.events[0].Type)
	}
	if sink.events[1].Type != models.EventTypeToolResult {
		t.Fatalf("expected TOOL_RESULT, got %q", sink.events[1].Type)
	}
}

func TestAuditHook_ClassifiedBashErrorExitCode(t *testing.T) {
	t.Parallel()

	sink := &mockEventSink{}
	mockSB := &mockExecSandbox{result: sandbox.Result{
		Success: false, ExitCode: 1,
		Stdout: "", Stderr: "command not found",
	}}

	w := NewWorker(
		&mockAgenticStore{},
		nil,
		mockSB,
		nil,
		sink,
		WorkerOptions{MaxToolIterations: 5},
	)

	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)
	call := gateway.ToolCall{
		ID:       "call_bash_err",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"bad"}`},
	}
	tr := w.dispatchToolWithProject(context.Background(), "task-bash-err", "proj-bash-err", call, nil, executor, nil, false)

	if tr.Status != ToolStatusError {
		t.Fatalf("expected error status, got %s", tr.Status)
	}
	forCtx := tr.ForContext()
	if !strings.Contains(forCtx, "command not found") {
		t.Fatalf("ForContext() = %q, want stderr in model context", forCtx)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(sink.events))
	}

	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1 for classified bash error", resultEvent.ExitCode)
	}
}
