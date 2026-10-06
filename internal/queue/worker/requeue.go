package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agentd/internal/models"
)

// requeueAttempts bounds how often a requeue chases a row that keeps moving.
// Each retry follows a lost optimistic lock, i.e. a concurrent writer that
// committed in between, so a handful is generous and the bound keeps a
// pathologically hot row from pinning the caller.
const requeueAttempts = 3

// RequeueStore is the slice of the board a requeue needs.
type RequeueStore interface {
	GetTask(ctx context.Context, id string) (*models.Task, error)
	UpdateTaskState(ctx context.Context, id string, expectedUpdatedAt time.Time, next models.TaskState) (*models.Task, error)
}

// RequeueTask moves task back to READY. The write carries the caller's
// updated_at, so a concurrent writer makes it fail with ErrOptimisticLock;
// the row is then re-read and the write retried against the version the store
// has now. The store checks the transition before the version, so a row moved
// somewhere READY is unreachable from fails with ErrInvalidStateTransition
// instead; it takes the same re-read path. A row that is no longer in one of the from states has been moved by
// someone else (a reconcile, an operator, a finished run) and is left alone.
func RequeueTask(ctx context.Context, store RequeueStore, task models.Task, from ...models.TaskState) error {
	expected := task.UpdatedAt
	for attempt := 0; attempt < requeueAttempts; attempt++ {
		_, err := store.UpdateTaskState(ctx, task.ID, expected, models.TaskStateReady)
		if !errors.Is(err, models.ErrOptimisticLock) && !errors.Is(err, models.ErrInvalidStateTransition) {
			return err
		}
		current, getErr := store.GetTask(ctx, task.ID)
		if getErr != nil {
			return fmt.Errorf("requeue: refresh after lost lock: %w", getErr)
		}
		if !stateIn(current.State, from) {
			return nil
		}
		expected = current.UpdatedAt
	}
	return fmt.Errorf("requeue task %s: %w after %d attempts", task.ID, models.ErrOptimisticLock, requeueAttempts)
}

func stateIn(state models.TaskState, set []models.TaskState) bool {
	for _, s := range set {
		if s == state {
			return true
		}
	}
	return false
}
