package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *Store) UpdateTaskDescription(
	ctx context.Context,
	id string,
	expectedUpdatedAt time.Time,
	description string,
) (*models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin task description update: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		now := utcNow()
		result, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET description = ?, updated_at = ?
			WHERE id = ? AND updated_at = ?`,
			description, formatTime(now), id, formatTime(expectedUpdatedAt))
		if err != nil {
			return nil, fmt.Errorf("update task description: %w", err)
		}
		if err := requireRowsAffected(result, 1, models.ErrStateConflict); err != nil {
			return nil, err
		}
		task, err := selectTaskByID(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return task, commitTx(tx, "task description update")
	})
}
