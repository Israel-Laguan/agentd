package kanban

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

// B-018, store half. queue.RequeueTask retries a requeue that lost the
// optimistic lock, and its tests drive that against testutil's
// ContendedStateStore, a fake. This pins the contract that fake claims to
// mirror: against the real store a stale version is ErrOptimisticLock, not
// ErrStateConflict (which the worker's requeue deliberately swallows), the row
// is left untouched, and a retry with the version the store has now lands.
func TestRequeueWithStaleVersionIsAnOptimisticLockNotAStateConflict(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "stale-requeue",
		Tasks:       siblingDrafts(1),
	}); err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimNextReadyTasks() = (%d tasks, %v)", len(claimed), err)
	}
	stale := claimed[0]

	// Another writer commits to the row: any write moves updated_at.
	if _, err := store.UpdateTaskDescription(ctx, stale.ID, stale.UpdatedAt, "touched"); err != nil {
		t.Fatalf("concurrent write error = %v", err)
	}

	_, err = store.UpdateTaskState(ctx, stale.ID, stale.UpdatedAt, models.TaskStateReady)
	if !errors.Is(err, models.ErrOptimisticLock) {
		t.Fatalf("stale requeue error = %v, want ErrOptimisticLock", err)
	}
	if errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("stale requeue error = %v also matches ErrStateConflict, which worker.requeue swallows", err)
	}
	current, err := store.GetTask(ctx, stale.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if current.State != models.TaskStateQueued {
		t.Fatalf("state after the dropped requeue = %s, want QUEUED (the write must not half-apply)", current.State)
	}

	requeued, err := store.UpdateTaskState(ctx, current.ID, current.UpdatedAt, models.TaskStateReady)
	if err != nil || requeued.State != models.TaskStateReady {
		t.Fatalf("retry with the current version = (%v, %v), want READY", requeued, err)
	}
}
