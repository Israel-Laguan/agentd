package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// --- AuditHook tests ---

func TestAuditHook_EmitsToolCallAndResult(t *testing.T) {
	t.Parallel()
	sink := &mockEventSink{}
	hook := AuditHook(sink, sandbox.NewScrubber(nil))
	ctx := HookContext{
		ToolName: "bash", Args: `{"command":"ls"}`, CallID: "call_42",
		SessionID: "task-1", ProjectID: "proj-1", Timestamp: time.Now().Add(-50 * time.Millisecond),
	}
	got, err := hook.Fn(ctx, `{"Success":true,"ExitCode":0,"Stdout":"file.txt\n","Stderr":""}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "file.txt") {
		t.Fatalf("audit hook should not mutate result, got %q", got)
	}
	assertAuditHookEvents(t, sink)
}

func assertAuditHookEvents(t *testing.T, sink *mockEventSink) {
	t.Helper()
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(sink.events))
	}
	if sink.events[0].Type != models.EventTypeToolCall || sink.events[1].Type != models.EventTypeToolResult {
		t.Fatalf("event types = %q, %q", sink.events[0].Type, sink.events[1].Type)
	}
	var callEvent ToolCallEvent
	if err := json.Unmarshal([]byte(sink.events[0].Payload), &callEvent); err != nil {
		t.Fatalf("unmarshal TOOL_CALL: %v", err)
	}
	if callEvent.ToolName != "bash" || callEvent.CallID != "call_42" {
		t.Fatalf("call event = %+v", callEvent)
	}
	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ToolName != "bash" || resultEvent.CallID != "call_42" || resultEvent.ExitCode != 0 {
		t.Fatalf("result event = %+v", resultEvent)
	}
	if sink.events[0].ProjectID != "proj-1" || sink.events[0].TaskID.String != "task-1" {
		t.Fatalf("event routing = proj %q task %q", sink.events[0].ProjectID, sink.events[0].TaskID.String)
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

// cancelAwareEventSink returns ctx.Err() when the context is already canceled,
// mimicking store.AppendEvent behavior on a canceled tool context.
type cancelAwareEventSink struct {
	events []models.Event
}

func (m *cancelAwareEventSink) Emit(ctx context.Context, ev models.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.events = append(m.events, ev)
	return nil
}

func TestAuditHook_EmitsWhenExecCtxCanceled(t *testing.T) {
	t.Parallel()
	sink := &cancelAwareEventSink{}
	hook := AuditHook(sink, nil)

	execCtx, cancel := context.WithCancel(context.Background())
	cancel()

	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"ls"}`,
		CallID:    "call_canceled",
		SessionID: "task-1",
		ProjectID: "proj-1",
		Timestamp: time.Now(),
		ExecCtx:   execCtx,
	}
	_, err := hook.Fn(ctx, `{"Success":true,"ExitCode":0}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events despite canceled ExecCtx, got %d", len(sink.events))
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
