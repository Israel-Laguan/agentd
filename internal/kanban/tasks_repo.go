package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *Store) GetTask(ctx context.Context, id string) (*models.Task, error) {
	return scanTask(s.db.QueryRowContext(ctx, selectTaskSQL()+" WHERE id = ?", id))
}

func (s *Store) ListTasksByProject(ctx context.Context, projectID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, selectTaskSQL()+" WHERE project_id = ? ORDER BY created_at", projectID)
	if err != nil {
		return nil, fmt.Errorf("list tasks by project: %w", err)
	}
	defer closeRows(rows)
	return scanTasks(rows)
}

func (s *Store) ListChildTasks(ctx context.Context, parentID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tasks.id, tasks.project_id, tasks.agent_id, tasks.title, tasks.description, tasks.state, tasks.assignee,
		       tasks.os_process_id, tasks.started_at, tasks.completed_at, tasks.last_heartbeat, tasks.retry_count, tasks.token_usage, tasks.created_at, tasks.updated_at
		FROM tasks
		INNER JOIN task_relations tr ON tr.child_task_id = tasks.id
		WHERE tr.parent_task_id = ?
		ORDER BY tasks.created_at`, parentID)
	if err != nil {
		return nil, fmt.Errorf("list child tasks: %w", err)
	}
	defer closeRows(rows)
	return scanTasks(rows)
}

func (s *Store) ClaimNextReadyTasks(ctx context.Context, limit int) ([]models.Task, error) {
	if limit <= 0 {
		limit = 1
	}
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin claim ready tasks: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		ids, err := selectReadyTaskIDs(ctx, tx, limit)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, commitTx(tx, "empty claim")
		}
		if err := queueReadyTaskIDs(ctx, tx, ids, utcNow()); err != nil {
			return nil, err
		}
		tasks, err := selectTasksByIDs(ctx, tx, ids)
		if err != nil {
			return nil, err
		}
		return tasks, commitTx(tx, "ready task claim")
	})
}

func (s *Store) UpdateTaskState(
	ctx context.Context,
	id string,
	expectedUpdatedAt time.Time,
	next models.TaskState,
) (*models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		current, err := s.GetTask(ctx, id)
		if err != nil {
			return nil, err
		}
		if !current.State.CanTransitionTo(next) {
			return nil, fmt.Errorf("%w: %s -> %s", models.ErrInvalidStateTransition, current.State, next)
		}
		return s.updateTaskState(ctx, current, expectedUpdatedAt, next)
	})
}

func (s *Store) UpdateTaskResult(
	ctx context.Context,
	id string,
	expectedUpdatedAt time.Time,
	result models.TaskResult,
) (*models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin task result update: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		now := utcNow()
		if err := updateTaskResultState(ctx, tx, id, expectedUpdatedAt, result.Success, now); err != nil {
			return nil, err
		}
		if err := finishTaskResultSideEffects(ctx, tx, id, result, now); err != nil {
			return nil, err
		}
		task, err := selectTaskByID(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return task, commitTx(tx, "task result update")
	})
}

func (s *Store) ReconcileGhostTasks(ctx context.Context, alivePIDs []int) ([]models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin ghost task reconciliation: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		ghosts, err := selectGhostTasks(ctx, tx, alivePIDs)
		if err != nil {
			return nil, err
		}
		if len(ghosts) == 0 {
			return nil, commitTx(tx, "empty ghost task reconciliation")
		}
		ids := make([]string, 0, len(ghosts))
		for _, task := range ghosts {
			ids = append(ids, task.ID)
		}
		now := utcNow()
		if err := resetGhostTasks(ctx, tx, ghosts, ids, now); err != nil {
			return nil, err
		}
		recovered, err := selectTasksByIDs(ctx, tx, ids)
		if err != nil {
			return nil, err
		}
		return recovered, commitTx(tx, "ghost task reconciliation")
	})
}

func (s *Store) ReconcileStaleTasks(ctx context.Context, alivePIDs []int, staleThreshold time.Duration) ([]models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin stale task reconciliation: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		now := utcNow()
		staleBefore := now.Add(-staleThreshold)
		stale, err := selectStaleTasks(ctx, tx, alivePIDs, staleBefore)
		if err != nil {
			return nil, err
		}
		if len(stale) == 0 {
			return nil, commitTx(tx, "empty stale task reconciliation")
		}
		ids := make([]string, 0, len(stale))
		for _, task := range stale {
			ids = append(ids, task.ID)
		}
		if err := resetGhostTasks(ctx, tx, stale, ids, now); err != nil {
			return nil, err
		}
		recovered, err := selectTasksByIDs(ctx, tx, ids)
		if err != nil {
			return nil, err
		}
		return recovered, commitTx(tx, "stale task reconciliation")
	})
}

func (s *Store) ReconcileOrphanedQueued(ctx context.Context, minAge time.Duration) ([]models.Task, error) {
	if minAge <= 0 {
		return nil, nil
	}
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin orphaned queued reconciliation: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		now := utcNow()
		staleBefore := now.Add(-minAge)
		orphaned, err := selectOrphanedQueuedTasks(ctx, tx, staleBefore)
		if err != nil {
			return nil, err
		}
		if len(orphaned) == 0 {
			return nil, commitTx(tx, "empty orphaned queued reconciliation")
		}
		ids := make([]string, 0, len(orphaned))
		for _, task := range orphaned {
			ids = append(ids, task.ID)
		}
		if err := resetGhostTasks(ctx, tx, orphaned, ids, now); err != nil {
			return nil, err
		}
		recovered, err := selectTasksByIDs(ctx, tx, ids)
		if err != nil {
			return nil, err
		}
		return recovered, commitTx(tx, "orphaned queued reconciliation")
	})
}
