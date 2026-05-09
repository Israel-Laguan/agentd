package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *Store) updateTaskState(
	ctx context.Context,
	current *models.Task,
	expectedUpdatedAt time.Time,
	next models.TaskState,
) (*models.Task, error) {
	now := utcNow()
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
	result, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, started_at = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND updated_at = ?`,
		string(next), nullableTime(startedAt), nullableTime(completedAt), formatTime(now), current.ID, formatTime(expectedUpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("update task state: %w", err)
	}
	return s.finishTaskStateUpdate(ctx, current.ID, result)
}

func (s *Store) finishTaskStateUpdate(ctx context.Context, id string, result rowsAffected) (*models.Task, error) {
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("read task state update count: %w", err)
	}
	if affected == 0 {
		return nil, models.ErrOptimisticLock
	}
	return s.GetTask(ctx, id)
}
