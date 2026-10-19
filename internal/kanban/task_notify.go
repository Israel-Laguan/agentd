package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

// WithTaskNotifier returns a shallow copy of the Store that calls n after every
// committed change to a task row, so live subscribers hear about it without each
// caller remembering to publish (B-074).
func (s *Store) WithTaskNotifier(n models.TaskChangeNotifier) *Store {
	cp := *s
	cp.notifier = n
	return &cp
}

// notifyTask announces a committed task change and passes the write's result
// through, so a write method can end with `return s.notifyTask(retryOnBusy(...))`.
// A failed write notifies nothing.
func (s *Store) notifyTask(task *models.Task, err error) (*models.Task, error) {
	if err == nil && task != nil {
		s.notify(*task)
	}
	return task, err
}

// notifyTasks is notifyTask for writes that change several rows.
func (s *Store) notifyTasks(tasks []models.Task, err error) ([]models.Task, error) {
	if err == nil {
		for _, task := range tasks {
			s.notify(task)
		}
	}
	return tasks, err
}

func (s *Store) notify(task models.Task) {
	if s.notifier != nil {
		s.notifier(task)
	}
}

// selectProjectTasksChangedAt returns the tasks of a project that the
// transaction stamped with now: the row a write was asked about plus whatever its
// side effects moved (unlocked children, unblocked parents, failed subtasks).
// A write announces these after commit so no changed row goes unannounced.
func selectProjectTasksChangedAt(ctx context.Context, tx *immediateTx, projectID string, now time.Time) ([]models.Task, error) {
	rows, err := tx.QueryContext(ctx,
		selectTaskSQL()+" WHERE project_id = ? AND updated_at = ? ORDER BY created_at", projectID, formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("select changed tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

// selectChangedInProjects is selectProjectTasksChangedAt over every project the
// given tasks belong to, each queried once.
func selectChangedInProjects(ctx context.Context, tx *immediateTx, tasks []models.Task, now time.Time) ([]models.Task, error) {
	var changed []models.Task
	seen := map[string]bool{}
	for _, task := range tasks {
		if seen[task.ProjectID] {
			continue
		}
		seen[task.ProjectID] = true
		rows, err := selectProjectTasksChangedAt(ctx, tx, task.ProjectID, now)
		if err != nil {
			return nil, err
		}
		changed = append(changed, rows...)
	}
	return changed, nil
}
