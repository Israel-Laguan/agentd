package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

// ListCompletedTasksOlderThan returns completed tasks the memory curator has
// not handled yet. A task is handled once any of its events is marked CURATED
// or the purge left its EVENTS_PURGED marker (purging deletes the CURATED rows,
// so without the marker the task would be curated again). A task with no
// events has nothing to curate and is not listed.
func (s *Store) ListCompletedTasksOlderThan(ctx context.Context, age time.Duration) ([]models.Task, error) {
	cutoff := utcNow().Add(-age)
	rows, err := s.db.QueryContext(ctx, selectTaskSQL()+`
		WHERE state = ? AND updated_at < ?
		AND EXISTS (SELECT 1 FROM events e WHERE e.task_id = tasks.id)
		AND NOT EXISTS (
			SELECT 1 FROM events e
			WHERE e.task_id = tasks.id
			AND (e.updated_at LIKE 'CURATED:%' OR e.type = ?)
		)
		ORDER BY updated_at`,
		models.TaskStateCompleted, formatTime(cutoff), "EVENTS_PURGED")
	if err != nil {
		return nil, fmt.Errorf("list completed tasks older than %v: %w", age, err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}
