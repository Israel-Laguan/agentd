package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
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
	hook := ApprovalGateHook([]string{"deploy"}, handler)

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

func TestApprovalGateHook_SuspendsGatedTool(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: false}
	hook := ApprovalGateHook([]string{"deploy", "write_config"}, handler)

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
		t.Fatal("gated tool should always be vetoed (suspension pattern)")
	}
	if !strings.Contains(verdict.Result, "paused pending human approval") {
		t.Fatalf("result should indicate suspension, got %q", verdict.Result)
	}
	if handler.called != 1 {
		t.Fatalf("handler.called = %d, want 1", handler.called)
	}
	if handler.lastReq.ToolName != "deploy" {
		t.Fatalf("lastReq.ToolName = %q, want %q", handler.lastReq.ToolName, "deploy")
	}
}

func TestApprovalGateHook_AllowsWhenApproved(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: true}
	hook := ApprovalGateHook([]string{"deploy"}, handler)

	verdict, err := hook.Fn(HookContext{
		ToolName:  "deploy",
		Args:      `{}`,
		SessionID: "task-123",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("approved gated tool should not be vetoed")
	}
}

func TestApprovalGateHook_BlocksGatedTool_Rejected(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: false, reason: "too risky"}
	hook := ApprovalGateHook([]string{"deploy"}, handler)

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

func TestApprovalGateHook_SuspendedNoReason(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: false, reason: ""}
	hook := ApprovalGateHook([]string{"deploy"}, handler)

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
		t.Fatal("gated tool should be vetoed")
	}
	if !strings.Contains(verdict.Result, "paused pending human approval") {
		t.Fatalf("result should indicate suspension, got %q", verdict.Result)
	}
}

func TestApprovalGateHook_HandlerError(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{err: errors.New("network failure")}
	hook := ApprovalGateHook([]string{"deploy"}, handler)

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

func TestApprovalGateHook_NilHandler(t *testing.T) {
	t.Parallel()
	hook := ApprovalGateHook([]string{"deploy"}, nil)

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
		t.Fatal("nil handler should veto gated tools")
	}
	if !strings.Contains(verdict.Reason, "approval handler not configured") {
		t.Fatalf("reason = %q, want handler not configured", verdict.Reason)
	}
}

func TestApprovalGateHook_EmptyGatedList(t *testing.T) {
	t.Parallel()
	handler := &stubApprovalHandler{approved: true}
	hook := ApprovalGateHook(nil, handler)

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
	hook := ApprovalGateHook(nil, nil)
	if hook.Name != "approval-gate" {
		t.Fatalf("hook.Name = %q, want %q", hook.Name, "approval-gate")
	}
	if hook.Policy != FailClosed {
		t.Fatal("approval gate should use FailClosed policy")
	}
}

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

// --- BlockingApprovalHandler ---

type approvalMockStore struct {
	models.KanbanStore
	expectedUpdatedAt time.Time
	blockCalled       bool
}

func (s *approvalMockStore) BlockTaskWithSubtasks(_ context.Context, _ string, expectedUpdatedAt time.Time, subtasks []models.DraftTask) (*models.Task, []models.Task, error) {
	s.blockCalled = true
	s.expectedUpdatedAt = expectedUpdatedAt
	created := make([]models.Task, len(subtasks))
	for i, d := range subtasks {
		created[i] = models.Task{
			BaseEntity: models.BaseEntity{ID: "sub-" + d.Title},
			Title:      d.Title,
			Assignee:   d.Assignee,
		}
	}
	return &models.Task{}, created, nil
}

func TestBlockingApprovalHandler_ReturnsNotApproved(t *testing.T) {
	t.Parallel()
	store := &approvalMockStore{}
	handler := NewBlockingApprovalHandler(store, nil)

	taskUpdatedAt := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	resp, err := handler.RequestApproval(context.Background(), ApprovalRequest{
		ToolName:      "deploy",
		Arguments:     `{"target":"prod"}`,
		TaskID:        "task-1",
		TaskUpdatedAt: taskUpdatedAt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Approved {
		t.Fatal("handler should return Approved=false (suspension pattern)")
	}
	if !store.blockCalled {
		t.Fatal("expected BlockTaskWithSubtasks to be called")
	}
	if !store.expectedUpdatedAt.Equal(taskUpdatedAt) {
		t.Fatalf("expectedUpdatedAt = %v, want %v", store.expectedUpdatedAt, taskUpdatedAt)
	}
}

func TestBlockingApprovalHandler_SuspendsViaHook(t *testing.T) {
	t.Parallel()
	taskUpdatedAt := time.Now()
	store := &approvalMockStore{}
	handler := NewBlockingApprovalHandler(store, nil)
	hook := ApprovalGateHook([]string{"deploy"}, handler)

	verdict, err := hook.Fn(HookContext{
		ToolName:      "deploy",
		Args:          `{}`,
		SessionID:     "task-1",
		TaskUpdatedAt: taskUpdatedAt,
		Timestamp:     time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("BlockingApprovalHandler returns Approved=false so hook should veto")
	}
	if !strings.Contains(verdict.Result, "paused pending human approval") {
		t.Fatalf("result = %q", verdict.Result)
	}
	if !store.expectedUpdatedAt.Equal(taskUpdatedAt) {
		t.Fatalf("expectedUpdatedAt = %v, want %v", store.expectedUpdatedAt, taskUpdatedAt)
	}
}
