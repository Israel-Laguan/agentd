package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
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
	if !verdict.Suspend {
		t.Fatal("blocked approval should set Suspend=true")
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
	*testutil.FakeKanbanStore
	expectedUpdatedAt time.Time
	blockCalled       bool
}

func (s *approvalMockStore) BlockTaskWithSubtasks(ctx context.Context, id string, expectedUpdatedAt time.Time, subtasks []models.DraftTask) (*models.Task, []models.Task, error) {
	s.blockCalled = true
	s.expectedUpdatedAt = expectedUpdatedAt
	return s.FakeKanbanStore.BlockTaskWithSubtasks(ctx, id, expectedUpdatedAt, subtasks)
}

func TestBlockingApprovalHandler_ReturnsNotApproved(t *testing.T) {
	t.Parallel()
	store := &approvalMockStore{FakeKanbanStore: testutil.NewFakeStore()}
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "p", Tasks: []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	handler := NewBlockingApprovalHandler(store)

	resp, err := handler.RequestApproval(context.Background(), ApprovalRequest{
		ToolName:      "deploy",
		Arguments:     `{"target":"prod"}`,
		TaskID:        parent.ID,
		TaskUpdatedAt: parent.UpdatedAt,
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
	if !store.expectedUpdatedAt.Equal(parent.UpdatedAt) {
		t.Fatalf("expectedUpdatedAt = %v, want %v", store.expectedUpdatedAt, parent.UpdatedAt)
	}
}

func TestBlockingApprovalHandler_GrantsCompletedApproval(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "p", Tasks: []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	_, created, err := store.BlockTaskWithSubtasks(context.Background(), parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title: approvalSubtaskTitle("deploy"), Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 subtask, got %d", len(created))
	}
	if _, err := store.UpdateTaskState(context.Background(), created[0].ID, created[0].UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete approval subtask: %v", err)
	}

	handler := NewBlockingApprovalHandler(store)
	resp, err := handler.RequestApproval(context.Background(), ApprovalRequest{
		ToolName: "deploy", TaskID: parent.ID, TaskUpdatedAt: parent.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Approved {
		t.Fatal("expected Approved=true for completed approval subtask")
	}
	children, err := store.ListChildTasks(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 approval subtask, got %d", len(children))
	}
}

func TestBlockingApprovalHandler_SuspendsViaHook(t *testing.T) {
	t.Parallel()
	store := &approvalMockStore{FakeKanbanStore: testutil.NewFakeStore()}
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "p", Tasks: []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	handler := NewBlockingApprovalHandler(store)
	hook := ApprovalGateHook([]string{"deploy"}, handler)

	verdict, err := hook.Fn(HookContext{
		ToolName:      "deploy",
		Args:          `{}`,
		SessionID:     parent.ID,
		TaskUpdatedAt: parent.UpdatedAt,
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
	if !verdict.Suspend {
		t.Fatal("expected Suspend=true")
	}
	if !store.expectedUpdatedAt.Equal(parent.UpdatedAt) {
		t.Fatalf("expectedUpdatedAt = %v, want %v", store.expectedUpdatedAt, parent.UpdatedAt)
	}
}
