package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// --- stubApprovalHandler ---

type stubApprovalHandler struct {
	approved bool
	reason   string
	err      error
	called   int
	lastReq  ApprovalRequest
}

func (s *stubApprovalHandler) RequestApproval(_ context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	s.called++
	s.lastReq = req
	if s.err != nil {
		return ApprovalResponse{}, s.err
	}
	return ApprovalResponse{Approved: s.approved, Reason: s.reason}, nil
}

// --- ApprovalGateHook ---

func TestApprovalGateHook_AllowsUngatedTool(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: true}
	hook := ApprovalGateHook([]string{"deploy"}, handler, nil, nil)

	verdict, err := hook.Fn(HookContext{ToolName: "bash", Args: `{"command":"ls"}`, Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("ungated tool should not be vetoed")
	}
	if handler.called != 0 {
		t.Fatal("handler should not be called for ungated tools")
	}
}

func TestApprovalGateHook_BlocksGatedTool_Approved(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: true}
	hook := ApprovalGateHook([]string{"deploy", "write_config"}, handler, nil, nil)

	verdict, err := hook.Fn(HookContext{
		ToolName:  "deploy",
		Args:      `{"target":"production"}`,
		SessionID: "task-123",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("approved tool call should not be vetoed")
	}
	if handler.called != 1 {
		t.Fatalf("handler.called = %d, want 1", handler.called)
	}
	if handler.lastReq.ToolName != "deploy" {
		t.Fatalf("lastReq.ToolName = %q, want %q", handler.lastReq.ToolName, "deploy")
	}
}

func TestApprovalGateHook_BlocksGatedTool_Rejected(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: false, reason: "too risky"}
	hook := ApprovalGateHook([]string{"deploy"}, handler, nil, nil)

	verdict, err := hook.Fn(HookContext{
		ToolName:  "deploy",
		Args:      `{"target":"production"}`,
		SessionID: "task-123",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("rejected tool call should be vetoed")
	}
	if !strings.Contains(verdict.Result, "APPROVAL REJECTED") {
		t.Fatalf("result should contain rejection message, got %q", verdict.Result)
	}
	if !strings.Contains(verdict.Result, "too risky") {
		t.Fatalf("result should contain rejection reason, got %q", verdict.Result)
	}
}

func TestApprovalGateHook_RejectedNoReason(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: false, reason: ""}
	hook := ApprovalGateHook([]string{"deploy"}, handler, nil, nil)

	verdict, err := hook.Fn(HookContext{
		ToolName:  "deploy",
		Args:      `{}`,
		SessionID: "task-123",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("rejected tool call should be vetoed")
	}
	if !strings.Contains(verdict.Result, "no reason provided") {
		t.Fatalf("result should mention 'no reason provided', got %q", verdict.Result)
	}
}

func TestApprovalGateHook_HandlerError(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{err: errors.New("network failure")}
	hook := ApprovalGateHook([]string{"deploy"}, handler, nil, nil)

	verdict, err := hook.Fn(HookContext{
		ToolName:  "deploy",
		Args:      `{}`,
		SessionID: "task-123",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("handler error should produce veto")
	}
	if !strings.Contains(verdict.Reason, "approval request failed") {
		t.Fatalf("reason should mention handler failure, got %q", verdict.Reason)
	}
}

func TestApprovalGateHook_EmptyGatedList(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: true}
	hook := ApprovalGateHook(nil, handler, nil, nil)

	verdict, err := hook.Fn(HookContext{ToolName: "deploy", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("empty gated list should allow all tools")
	}
	if handler.called != 0 {
		t.Fatal("handler should not be called with empty gated list")
	}
}

func TestApprovalGateHook_HookName(t *testing.T) {
	t.Parallel()
	hook := ApprovalGateHook(nil, nil, nil, nil)
	if hook.Name != "approval-gate" {
		t.Fatalf("hook.Name = %q, want %q", hook.Name, "approval-gate")
	}
	if hook.Policy != FailClosed {
		t.Fatal("approval gate should use FailClosed policy")
	}
}

// --- FormatRejection ---

func TestFormatRejection_ContainsToolAndReason(t *testing.T) {
	t.Parallel()
	result := formatRejection("deploy", "not authorized")
	if !strings.Contains(result, "deploy") {
		t.Fatalf("result should contain tool name, got %q", result)
	}
	if !strings.Contains(result, "not authorized") {
		t.Fatalf("result should contain reason, got %q", result)
	}
	if !strings.Contains(result, "APPROVAL REJECTED") {
		t.Fatalf("result should contain APPROVAL REJECTED header, got %q", result)
	}
}
