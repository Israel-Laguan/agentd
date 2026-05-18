package worker

import (
	"context"
	"encoding/json"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

func TestAuditHook_DelegateJSONErrorExitCode(t *testing.T) {
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
	call := gateway.ToolCall{
		ID:       "call_delegate_err",
		Function: gateway.ToolCallFunction{Name: "delegate", Arguments: `{}`},
	}
	tr := w.dispatchToolWithProject(context.Background(), "task-delegate-err", "proj-delegate-err", call, nil, executor, nil)

	if tr.Status != ToolStatusError {
		t.Fatalf("expected error status, got %s", tr.Status)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(sink.events))
	}

	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1 for delegate jsonErrorf", resultEvent.ExitCode)
	}
}

func TestAuditHook_CapabilityJSONErrorExitCode(t *testing.T) {
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
	call := gateway.ToolCall{
		ID:       "call_cap_err",
		Function: gateway.ToolCallFunction{Name: "nonexistent_capability", Arguments: `{}`},
	}
	tr := w.dispatchToolWithProject(context.Background(), "task-cap-err", "proj-cap-err", call, nil, executor, nil)

	if tr.Status != ToolStatusError {
		t.Fatalf("expected error status, got %s", tr.Status)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(sink.events))
	}

	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1 for capability jsonErrorf", resultEvent.ExitCode)
	}
}

func TestAuditHook_BashExitCode127(t *testing.T) {
	t.Parallel()

	sink := &mockEventSink{}
	mockSB := &mockExecSandbox{result: sandbox.Result{
		Success: false, ExitCode: 127,
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
		ID:       "call_bash_127",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"missing-cmd"}`},
	}
	tr := w.dispatchToolWithProject(context.Background(), "task-bash-127", "proj-bash-127", call, nil, executor, nil)

	if tr.Status != ToolStatusError {
		t.Fatalf("expected error status, got %s", tr.Status)
	}
	if !tr.ExitCodeSet || tr.ExitCode != 127 {
		t.Fatalf("ExitCode = %d, ExitCodeSet = %v, want 127/true", tr.ExitCode, tr.ExitCodeSet)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(sink.events))
	}

	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ExitCode != 127 {
		t.Fatalf("ExitCode = %d, want 127", resultEvent.ExitCode)
	}
}
