package kanban

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

// CountTasksByState returns one row per (project, state) with the number of
// tasks in it, in a single grouped query. Unless includeHealing is set the
// self-healing manual-review handoffs are left out, as ListTasks does.
func (s *Store) CountTasksByState(ctx context.Context, includeHealing bool) ([]models.TaskStateCount, error) {
	query := `SELECT project_id, state, COUNT(*) FROM tasks`
	var args []any
	if !includeHealing {
		exclude, excludeArgs := selfHealingHandoffExcludeSQL()
		query += " WHERE " + exclude
		args = excludeArgs
	}
	rows, err := s.db.QueryContext(ctx, query+` GROUP BY project_id, state ORDER BY project_id, state`, args...)
	if err != nil {
		return nil, fmt.Errorf("count tasks by state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []models.TaskStateCount
	for rows.Next() {
		var c models.TaskStateCount
		if err := rows.Scan(&c.ProjectID, &c.State, &c.Count); err != nil {
			return nil, fmt.Errorf("scan task state count: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListAttentionTasks returns the open tasks a human may need to act on: those
// assigned to a human, plus BLOCKED, FAILED_REQUIRES_HUMAN, IN_CONSIDERATION
// and NEEDS_CONTEXT ones. Plain READY, RUNNING and finished tasks are not
// loaded.
func (s *Store) ListAttentionTasks(ctx context.Context) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, selectTaskSQL()+`
		WHERE state NOT IN (?, ?)
		AND (assignee = ? OR state IN (?, ?, ?, ?))
		ORDER BY created_at`,
		models.TaskStateCompleted, models.TaskStateFailed, models.TaskAssigneeHuman,
		models.TaskStateBlocked, models.TaskStateFailedRequiresHuman,
		models.TaskStateInConsideration, models.TaskStateNeedsContext)
	if err != nil {
		return nil, fmt.Errorf("list attention tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}
