package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

// UpdateTaskPatch applies an optional state transition and an optional description
// change in a single database transaction. Either pointer may be nil to leave the
// corresponding field unchanged. When both are non-nil the operation is
// all-or-nothing: a failure at any point rolls back the entire transaction so the
// task row is left exactly as it was before the call.
func (s *Store) UpdateTaskPatch(
	ctx context.Context,
	id string,
	expectedUpdatedAt time.Time,
	state *models.TaskState,
	description *string,
) (*models.Task, error) {
	var changed []models.Task
	task, err := retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		changed = nil
		current, err := s.GetTask(ctx, id)
		if err != nil {
			return nil, err
		}
		if !current.UpdatedAt.Equal(expectedUpdatedAt) {
			return nil, models.ErrStateConflict
		}
		if state != nil && !current.State.CanTransitionTo(*state) {
			return nil, fmt.Errorf("%w: %s -> %s", models.ErrInvalidStateTransition, current.State, *state)
		}

		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin task patch: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		now := utcNow()
		// rowUpdatedAt tracks the current updated_at value of the row inside this
		// transaction so each subsequent write uses the correct optimistic-lock
		// sentinel. After a successful state write the row's updated_at becomes now.
		rowUpdatedAt := expectedUpdatedAt

		if state != nil {
			if err := updateTaskStateInTx(ctx, tx, current, rowUpdatedAt, *state, now); err != nil {
				return nil, err
			}
			if err := finishTaskStateSideEffects(ctx, tx, id, *state, now); err != nil {
				return nil, err
			}
			rowUpdatedAt = now
		}

		if description != nil {
			if err := updateDescriptionInTx(ctx, tx, id, *description, now, rowUpdatedAt); err != nil {
				return nil, err
			}
		}

		task, err := selectTaskByID(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if changed, err = selectProjectTasksChangedAt(ctx, tx, task.ProjectID, now); err != nil {
			return nil, err
		}
		return task, commitTx(tx, "task patch")
	})
	_, _ = s.notifyTasks(changed, err)
	return task, err
}

// updateDescriptionInTx writes the description under the same optimistic lock
// as the state write that may precede it in the transaction.
func updateDescriptionInTx(ctx context.Context, tx *immediateTx, id, description string, now, rowUpdatedAt time.Time) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET description = ?, updated_at = ?
		WHERE id = ? AND updated_at = ?`,
		description, formatTime(now), id, formatTime(rowUpdatedAt))
	if err != nil {
		return fmt.Errorf("update task description: %w", err)
	}
	return requireRowsAffected(result, 1, models.ErrStateConflict)
}
