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

// --- ScrubResultHook tests ---

func TestScrubResultHook_RedactsAPIKey(t *testing.T) {
	t.Parallel()
	scrubber := sandbox.NewScrubber(nil)
	hook := ScrubResultHook(scrubber)

	input := "output with sk-AAAAAAAAAAAAAAAAAAAAAA key"
	got, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "sk-AAAA") {
		t.Fatalf("expected API key to be scrubbed, got %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] token, got %q", got)
	}
}

func TestScrubResultHook_NilScrubberPassthrough(t *testing.T) {
	t.Parallel()
	hook := ScrubResultHook(nil)
	input := "pass through sk-SECRET123"
	got, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Fatalf("nil scrubber should passthrough, got %q", got)
	}
}

func TestScrubResultHook_FailClosedPolicy(t *testing.T) {
	t.Parallel()
	hook := ScrubResultHook(sandbox.NewScrubber(nil))
	if hook.Policy != FailClosed {
		t.Fatalf("expected FailClosed, got %v", hook.Policy)
	}
}

func TestScrubResultHook_CleanInputUnchanged(t *testing.T) {
	t.Parallel()
	scrubber := sandbox.NewScrubber(nil)
	hook := ScrubResultHook(scrubber)
	input := "clean output with no secrets"
	got, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Fatalf("clean input should be unchanged, got %q", got)
	}
}

func TestScrubResultHook_IntegrationViaHookChain(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(ScrubResultHook(sandbox.NewScrubber(nil)))

	result := hc.RunPost(
		HookContext{ToolName: "bash", Timestamp: time.Now()},
		"token=sk-AAAAAAAAAAAAAAAAAAAAAA found",
	)
	if strings.Contains(result, "sk-AAAA") {
		t.Fatalf("API key not scrubbed through hook chain: %q", result)
	}
}

func TestScrubResultHook_ScrubsBeforeModelContext(t *testing.T) {
	t.Parallel()

	scrubber := sandbox.NewScrubber(nil)
	hc := NewHookChain()
	hc.RegisterPost(ScrubResultHook(scrubber))

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "key=sk-AAAAAAAAAAAAAAAAAAAAAA\n", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo key"}`},
	}

	result := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if strings.Contains(result, "sk-AAAA") {
		t.Fatalf("expected scrubbed result before model context, got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in result, got %q", result)
	}
}

func TestScrubResultHook_CustomPatterns(t *testing.T) {
	t.Parallel()
	scrubber := sandbox.NewScrubber([]string{`my-custom-secret-\d+`})
	hook := ScrubResultHook(scrubber)

	input := "found my-custom-secret-42 in config"
	got, err := hook.Fn(HookContext{ToolName: "read", Timestamp: time.Now()}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "my-custom-secret-42") {
		t.Fatalf("custom pattern not scrubbed: %q", got)
	}
}

// --- AuditHook tests ---

func TestAuditHook_EmitsToolCallAndResult(t *testing.T) {
	t.Parallel()
	sink := &mockEventSink{}
	scrubber := sandbox.NewScrubber(nil)
	hook := AuditHook(sink, scrubber)

	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"ls"}`,
		CallID:    "call_42",
		SessionID: "task-1",
		ProjectID: "proj-1",
		Timestamp: time.Now().Add(-50 * time.Millisecond),
	}

	got, err := hook.Fn(ctx, `{"Success":true,"ExitCode":0,"Stdout":"file.txt\n","Stderr":""}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not mutate result
	if !strings.Contains(got, "file.txt") {
		t.Fatalf("audit hook should not mutate result, got %q", got)
	}

	// Should emit exactly 2 events: TOOL_CALL + TOOL_RESULT
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(sink.events))
	}

	if sink.events[0].Type != models.EventTypeToolCall {
		t.Fatalf("expected TOOL_CALL event, got %q", sink.events[0].Type)
	}
	if sink.events[1].Type != models.EventTypeToolResult {
		t.Fatalf("expected TOOL_RESULT event, got %q", sink.events[1].Type)
	}

	// Verify TOOL_CALL payload
	var callEvent ToolCallEvent
	if err := json.Unmarshal([]byte(sink.events[0].Payload), &callEvent); err != nil {
		t.Fatalf("unmarshal TOOL_CALL: %v", err)
	}
	if callEvent.ToolName != "bash" {
		t.Fatalf("expected tool_name 'bash', got %q", callEvent.ToolName)
	}
	if callEvent.CallID != "call_42" {
		t.Fatalf("expected call_id 'call_42', got %q", callEvent.CallID)
	}

	// Verify TOOL_RESULT payload
	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ToolName != "bash" {
		t.Fatalf("expected tool_name 'bash', got %q", resultEvent.ToolName)
	}
	if resultEvent.CallID != "call_42" {
		t.Fatalf("expected call_id 'call_42', got %q", resultEvent.CallID)
	}
	if resultEvent.ExitCode != 0 {
		t.Fatalf("expected exit_code 0, got %d", resultEvent.ExitCode)
	}
	if resultEvent.DurationMs < 0 {
		t.Fatalf("expected non-negative duration, got %d", resultEvent.DurationMs)
	}

	// Verify project/task IDs on events
	if sink.events[0].ProjectID != "proj-1" {
		t.Fatalf("expected ProjectID 'proj-1', got %q", sink.events[0].ProjectID)
	}
	if sink.events[0].TaskID.String != "task-1" {
		t.Fatalf("expected TaskID 'task-1', got %q", sink.events[0].TaskID.String)
	}
}

func TestAuditHook_NilSinkPassthrough(t *testing.T) {
	t.Parallel()
	hook := AuditHook(nil, nil)
	got, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()}, "result")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "result" {
		t.Fatalf("nil sink should passthrough, got %q", got)
	}
}

func TestAuditHook_FailOpenPolicy(t *testing.T) {
	t.Parallel()
	hook := AuditHook(&mockEventSink{}, nil)
	if hook.Policy != FailOpen {
		t.Fatalf("expected FailOpen, got %v", hook.Policy)
	}
}

func TestAuditHook_ScrubsEventPayloads(t *testing.T) {
	t.Parallel()
	sink := &mockEventSink{}
	scrubber := sandbox.NewScrubber(nil)
	hook := AuditHook(sink, scrubber)

	secretArgs := `{"command":"export API_KEY=sk-AAAAAAAAAAAAAAAAAAAAAA"}`
	secretResult := "output with token=sk-BBBBBBBBBBBBBBBBBBBBBB"

	_, _ = hook.Fn(HookContext{
		ToolName:  "bash",
		Args:      secretArgs,
		CallID:    "call_secret",
		SessionID: "task-2",
		ProjectID: "proj-2",
		Timestamp: time.Now(),
	}, secretResult)

	if len(sink.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(sink.events))
	}

	// TOOL_CALL payload should have scrubbed arguments
	var callEvent ToolCallEvent
	if err := json.Unmarshal([]byte(sink.events[0].Payload), &callEvent); err != nil {
		t.Fatalf("unmarshal TOOL_CALL: %v", err)
	}
	if strings.Contains(callEvent.ArgumentsSummary, "sk-AAAA") {
		t.Fatalf("arguments should be scrubbed: %q", callEvent.ArgumentsSummary)
	}

	// TOOL_RESULT payload should have scrubbed output
	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if strings.Contains(resultEvent.OutputSummary, "sk-BBBB") {
		t.Fatalf("output should be scrubbed: %q", resultEvent.OutputSummary)
	}
}

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

	result := w.dispatchToolWithProject(
		context.Background(), "task-int", "proj-int", call, nil, executor,
	)

	// Result should be scrubbed (ScrubResultHook runs first)
	if strings.Contains(result, "sk-AAAA") {
		t.Fatalf("result entering model context should be scrubbed: %q", result)
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
	w.DispatchTool(context.Background(), "task-a", call1, nil, executor)

	// Call through dispatchToolWithProject
	call2 := gateway.ToolCall{
		ID:       "call_b",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo b"}`},
	}
	w.dispatchToolWithProject(context.Background(), "task-b", "proj-b", call2, nil, executor)

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
	result := w.dispatchToolWithProject(context.Background(), "task-err", "proj-err", call, nil, executor)

	// Result should contain error message
	if !strings.Contains(result, "unknown tool") {
		t.Fatalf("expected unknown tool error, got %q", result)
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
