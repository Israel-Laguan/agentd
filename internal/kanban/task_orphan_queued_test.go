package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestReconcileOrphanedQueuedResetsStaleClaim(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "orphan-queued",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	taskID := tasks[0].ID

	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0].State != models.TaskStateQueued {
		t.Fatalf("claimed = %#v, want one QUEUED task", claimed)
	}

	staleAt := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := store.db.ExecContext(ctx,
		`UPDATE tasks SET updated_at = ? WHERE id = ?`,
		formatTime(staleAt), taskID); err != nil {
		t.Fatalf("backdate updated_at: %v", err)
	}

	recovered, err := store.ReconcileOrphanedQueued(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ReconcileOrphanedQueued() error = %v", err)
	}
	if len(recovered) != 1 {
		t.Fatalf("recovered = %d, want 1", len(recovered))
	}
	assertTaskReadyWithoutHeartbeat(t, store, ctx, taskID)
}

func TestReconcileOrphanedQueuedSkipsRecentClaim(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "orphan-queued-recent",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	if _, err := store.ClaimNextReadyTasks(ctx, 1); err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}

	recovered, err := store.ReconcileOrphanedQueued(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ReconcileOrphanedQueued() error = %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("recovered = %d, want 0 for fresh QUEUED claim", len(recovered))
	}
	task, err := store.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.State != models.TaskStateQueued {
		t.Fatalf("state = %s, want QUEUED", task.State)
	}
}
