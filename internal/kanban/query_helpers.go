package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func updateTaskStateInTx(
	ctx context.Context,
	tx *immediateTx,
	current *models.Task,
	expectedUpdatedAt time.Time,
	next models.TaskState,
	now time.Time,
) error {
	startedAt := current.StartedAt
	if next == models.TaskStateRunning && startedAt == nil {
		startedAt = &now
	}
	var completedAt *time.Time
	switch next {
	case models.TaskStateCompleted, models.TaskStateFailed, models.TaskStateFailedRequiresHuman:
		completedAt = &now
	default:
		completedAt = nil
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, started_at = ?, completed_at = ?,
		    os_process_id = CASE WHEN ? = ? THEN NULL ELSE os_process_id END,
		    last_heartbeat = CASE WHEN ? = ? THEN NULL ELSE last_heartbeat END,
		    updated_at = ?
		WHERE id = ? AND updated_at = ?`,
		string(next), nullableTime(startedAt), nullableTime(completedAt),
		string(next), string(models.TaskStateBlocked),
		string(next), string(models.TaskStateBlocked),
		formatTime(now), current.ID, formatTime(expectedUpdatedAt))
	if err != nil {
		return fmt.Errorf("update task state: %w", err)
	}
	return requireRowsAffected(result, 1, models.ErrOptimisticLock)
}

func finishTaskStateSideEffects(ctx context.Context, tx *immediateTx, id string, next models.TaskState, now time.Time) error {
	switch next {
	case models.TaskStateCompleted, models.TaskStateFailed:
		return unblockBlockedParentsWhenChildrenResolved(ctx, tx, id, now)
	default:
		return nil
	}
}
