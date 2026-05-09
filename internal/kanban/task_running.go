package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *Store) MarkTaskRunning(
	ctx context.Context,
	id string,
	expectedUpdatedAt time.Time,
	pid int,
) (*models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		now := utcNow()
		result, err := s.db.ExecContext(ctx, `
			UPDATE tasks
			SET state = ?, os_process_id = ?, started_at = ?, completed_at = NULL, last_heartbeat = ?, updated_at = ?
			WHERE id = ? AND updated_at = ? AND state = ?`,
			models.TaskStateRunning, pid, formatTime(now), formatTime(now), formatTime(now),
			id, formatTime(expectedUpdatedAt), models.TaskStateQueued)
		if err != nil {
			return nil, fmt.Errorf("mark task running: %w", err)
		}
		if err := requireRowsAffected(result, 1, models.ErrStateConflict); err != nil {
			return nil, err
		}
		return s.GetTask(ctx, id)
	})
}

func (s *Store) UpdateTaskHeartbeat(ctx context.Context, id string) error {
	return retryOnBusyNoResult(ctx, func(ctx context.Context) error {
		result, err := s.db.ExecContext(ctx, `
			UPDATE tasks
			SET last_heartbeat = ?
			WHERE id = ? AND state = ?`,
			formatTime(utcNow()), id, models.TaskStateRunning)
		if err != nil {
			return fmt.Errorf("update task heartbeat: %w", err)
		}
		return requireRowsAffected(result, 1, models.ErrStateConflict)
	})
}
