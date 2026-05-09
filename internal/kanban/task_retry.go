package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *Store) IncrementRetryCount(
	ctx context.Context,
	id string,
	expectedUpdatedAt time.Time,
) (*models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		now := utcNow()
		result, err := s.db.ExecContext(ctx, `
			UPDATE tasks
			SET retry_count = retry_count + 1, updated_at = ?
			WHERE id = ? AND updated_at = ? AND state = ?`,
			formatTime(now), id, formatTime(expectedUpdatedAt), models.TaskStateRunning)
		if err != nil {
			return nil, fmt.Errorf("increment retry count: %w", err)
		}
		if err := requireRowsAffected(result, 1, models.ErrStateConflict); err != nil {
			return nil, err
		}
		return s.GetTask(ctx, id)
	})
}
