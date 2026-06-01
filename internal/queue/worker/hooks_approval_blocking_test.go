package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	agenthooks "agentd/internal/agent/hooks"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

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

func TestBlockingApprovalHandler_GrantsCompletedApprovalDespiteExpiredComments(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "p", Tasks: []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	expired := time.Now().Add(-time.Minute)
	if err := store.AddComment(context.Background(), models.Comment{
		TaskID: parent.ID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   models.HITLExpiresAtCommentPrefix + expired.UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("add expiry comment: %v", err)
	}
	_, created, err := store.BlockTaskWithSubtasks(context.Background(), parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title: approvalSubtaskTitle("deploy"), Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
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
		t.Fatal("expected Approved=true for completed approval despite expired comments")
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

func TestBlockingApprovalHandler_RejectionConsumedOnce(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p", Tasks: []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	_, created, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title: approvalSubtaskTitle("deploy"), Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	approval := created[0]
	const rejectionReason = "too risky for production"
	if err := store.AddComment(ctx, models.Comment{
		TaskID: approval.ID,
		Author: models.CommentAuthorUser,
		Body:   rejectionReason,
	}); err != nil {
		t.Fatalf("add rejection comment: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, approval.ID, approval.UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail approval subtask: %v", err)
	}

	handler := NewBlockingApprovalHandler(store)
	req := ApprovalRequest{
		ToolName: "deploy", Arguments: `{"target":"prod"}`, TaskID: parent.ID, TaskUpdatedAt: parent.UpdatedAt,
	}

	first, err := handler.RequestApproval(ctx, req)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	if first.Approved {
		t.Fatal("expected Approved=false after rejection")
	}
	if first.Reason != rejectionReason {
		t.Fatalf("first reason = %q, want %q", first.Reason, rejectionReason)
	}

	second, err := handler.RequestApproval(ctx, req)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if second.Approved {
		t.Fatal("expected Approved=false (suspension) on second request")
	}
	if second.Reason != "" {
		t.Fatalf("second reason = %q, want empty (fall through to new approval, not stale rejection)", second.Reason)
	}
}

func TestBlockingApprovalHandler_RejectionAllowsNewApprovalSubtask(t *testing.T) {
	t.Parallel()
	store := &approvalMockStore{FakeKanbanStore: testutil.NewFakeStore()}
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p", Tasks: []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	_, created, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title: approvalSubtaskTitle("deploy"), Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, created[0].ID, created[0].UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail approval subtask: %v", err)
	}

	handler := NewBlockingApprovalHandler(store)
	req := ApprovalRequest{
		ToolName: "deploy", Arguments: `{"target":"staging"}`, TaskID: parent.ID, TaskUpdatedAt: parent.UpdatedAt,
	}

	if _, err := handler.RequestApproval(ctx, req); err != nil {
		t.Fatalf("consume rejection: %v", err)
	}
	store.blockCalled = false

	if _, err := handler.RequestApproval(ctx, req); err != nil {
		t.Fatalf("request new approval: %v", err)
	}
	if !store.blockCalled {
		t.Fatal("expected BlockTaskWithSubtasks for new approval after rejection consumed")
	}
	children, err := store.ListChildTasks(ctx, parent.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	var approvalCount int
	for _, child := range children {
		if strings.HasPrefix(child.Title, models.HITLSubtaskTitleApproveTool) {
			approvalCount++
		}
	}
	if approvalCount != 2 {
		t.Fatalf("approval subtasks = %d, want 2 (original failed + new pending)", approvalCount)
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

	verdict, err := hook.Fn(agenthooks.HookContext{
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
