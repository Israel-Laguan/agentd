package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestReconcileExpiredBlockedTasks_FailsParentPastExpiry(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	parent := seedTestTask(t, store, "hitl-parent", models.TaskStateRunning)

	expired := time.Now().Add(-time.Minute)
	if err := store.AddComment(ctx, models.Comment{
		TaskID: parent.ID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlExpiresAtPrefix + expired.UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("add expiry comment: %v", err)
	}

	blocked, _, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:       "Approve tool call: deploy",
		Description: "review",
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	failed, err := store.ReconcileExpiredBlockedTasks(ctx, time.Now())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("expired tasks = %d, want 1", len(failed))
	}
	if failed[0].State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state = %s, want FAILED_REQUIRES_HUMAN", failed[0].State)
	}
	after, err := store.GetTask(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if after.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("parent state = %s, want FAILED_REQUIRES_HUMAN", after.State)
	}
}

func TestReconcileExpiredBlockedTasks_SkipsBlockedWithoutExpiry(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	parent := seedTestTask(t, store, "hitl-no-expiry", models.TaskStateRunning)

	blocked, _, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:       "Manual review required: AI providers unavailable",
		Description: "review",
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	afterBlock, err := store.GetTask(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("get blocked parent: %v", err)
	}

	failed, err := store.ReconcileExpiredBlockedTasks(ctx, afterBlock.UpdatedAt.Add(31*time.Minute))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("expired tasks = %d, want 0", len(failed))
	}
	after, err := store.GetTask(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if after.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", after.State)
	}
}

func TestReconcileExpiredBlockedTasks_SkipsBlockedWithOnlyTerminalChildren(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	parent := seedTestTask(t, store, "hitl-terminal-children", models.TaskStateRunning)

	expired := time.Now().Add(-time.Minute)
	if err := store.AddComment(ctx, models.Comment{
		TaskID: parent.ID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlExpiresAtPrefix + expired.UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("add expiry comment: %v", err)
	}

	blocked, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:       "Approve tool call: deploy",
		Description: "review",
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	child, err := store.GetTask(ctx, children[0].ID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, child.ID, child.UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail child: %v", err)
	}

	failed, err := store.ReconcileExpiredBlockedTasks(ctx, time.Now())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("expired tasks = %d, want 0", len(failed))
	}
	after, err := store.GetTask(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if after.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", after.State)
	}
}
