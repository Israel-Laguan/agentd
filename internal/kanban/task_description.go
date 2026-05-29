package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
	kdb "agentd/internal/kanban/db"
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

		now := kdb.UTCNow()
		result, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET description = ?, updated_at = ?
			WHERE id = ? AND updated_at = ?`,
			description, kdb.FormatTime(now), id, kdb.FormatTime(expectedUpdatedAt))
		if err != nil {
			return nil, fmt.Errorf("update task description: %w", err)
		}
		if err := kdb.RequireRowsAffected(result, 1, models.ErrStateConflict); err != nil {
			return nil, err
		}
		task, err := kdb.SelectTaskByID(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return task, commitTx(tx, "task description update")
	})
}
