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
