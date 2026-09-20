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
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

func (s *Store) ListChildTasks(ctx context.Context, parentID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, taskSelectColumns("tasks")+`
		FROM tasks
		INNER JOIN task_relations tr ON tr.child_task_id = tasks.id
		WHERE tr.parent_task_id = ?
		ORDER BY tasks.created_at`, parentID)
	if err != nil {
		return nil, fmt.Errorf("list child tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

func (s *Store) ListParentTasks(ctx context.Context, childID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, taskSelectColumns("tasks")+`
		FROM tasks
		INNER JOIN task_relations tr ON tr.parent_task_id = tasks.id
		WHERE tr.child_task_id = ?
		ORDER BY tasks.created_at`, childID)
	if err != nil {
		return nil, fmt.Errorf("list parent tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

func (s *Store) ListParentTasksByRelation(ctx context.Context, childID string, relationType models.TaskRelationType) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, taskSelectColumns("tasks")+`
		FROM tasks
		INNER JOIN task_relations tr ON tr.parent_task_id = tasks.id
		WHERE tr.child_task_id = ? AND tr.relation_type = ?
		ORDER BY tasks.created_at`, childID, string(relationType))
	if err != nil {
		return nil, fmt.Errorf("list parent tasks by relation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

func (s *Store) ListChildTasksByRelation(ctx context.Context, parentID string, relationType models.TaskRelationType) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, taskSelectColumns("tasks")+`
		FROM tasks
		INNER JOIN task_relations tr ON tr.child_task_id = tasks.id
		WHERE tr.parent_task_id = ? AND tr.relation_type = ?
		ORDER BY tasks.created_at`, parentID, string(relationType))
	if err != nil {
		return nil, fmt.Errorf("list child tasks by relation: %w", err)
	}
	defer func() { _ = rows.Close() }()
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
		defer func() { _ = tx.Rollback() }()

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

func (s *Store) MarkProjectTasksReady(ctx context.Context, projectID string) ([]models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin mark project tasks ready: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		now := utcNow()
		_, err = tx.ExecContext(ctx, `
			UPDATE tasks SET state = ?, updated_at = ?
			WHERE project_id = ? AND state = ?
			AND NOT EXISTS (
				SELECT 1 FROM task_relations tr
				JOIN tasks parent ON parent.id = tr.parent_task_id
				WHERE tr.child_task_id = tasks.id
				AND parent.state NOT IN (?, ?)
			)`,
			models.TaskStateReady, formatTime(now), projectID, models.TaskStatePending,
			models.TaskStateCompleted, models.TaskStateFailed)
		if err != nil {
			return nil, fmt.Errorf("mark project tasks ready: %w", err)
		}
		rows, err := tx.QueryContext(ctx,
			selectTaskSQL()+" WHERE project_id = ? AND state = ? ORDER BY created_at",
			projectID, models.TaskStateReady)
		if err != nil {
			return nil, fmt.Errorf("select unlocked tasks: %w", err)
		}
		defer func() { _ = rows.Close() }()
		tasks, err := scanTasks(rows)
		if err != nil {
			return nil, err
		}
		return tasks, commitTx(tx, "mark project tasks ready")
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
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin task state update: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		now := utcNow()
		if err := updateTaskStateInTx(ctx, tx, current, expectedUpdatedAt, next, now); err != nil {
			return nil, err
		}
		if err := finishTaskStateSideEffects(ctx, tx, id, next, now); err != nil {
			return nil, err
		}
		task, err := selectTaskByID(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return task, commitTx(tx, "task state update")
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
		defer func() { _ = tx.Rollback() }()

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

func (s *Store) CompleteTieredOrigin(
	ctx context.Context,
	id string,
	expectedUpdatedAt time.Time,
	result models.TaskResult,
) (*models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin tiered origin completion: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		now := utcNow()
		if err := completeTieredOriginState(ctx, tx, id, expectedUpdatedAt, result.Success, now); err != nil {
			return nil, err
		}
		if err := finishTaskResultSideEffects(ctx, tx, id, result, now); err != nil {
			return nil, err
		}
		task, err := selectTaskByID(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return task, commitTx(tx, "tiered origin completion")
	})
}

func (s *Store) ReconcileGhostTasks(ctx context.Context, alivePIDs []int) ([]models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin ghost task reconciliation: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		ghosts, err := selectGhostTasks(ctx, tx, alivePIDs)
		if err != nil {
			return nil, err
		}
		return reconcileAndRecover(ctx, tx, ghosts, resetGhostTasks, "ghost task reconciliation")
	})
}
func (s *Store) ReconcileStaleTasks(ctx context.Context, alivePIDs []int, staleThreshold time.Duration) ([]models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin stale task reconciliation: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		now := utcNow()
		staleBefore := now.Add(-staleThreshold)
		stale, err := selectStaleTasks(ctx, tx, alivePIDs, staleBefore)
		if err != nil {
			return nil, err
		}
		return reconcileAndRecover(ctx, tx, stale, resetGhostTasks, "stale task reconciliation")
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
		defer func() { _ = tx.Rollback() }()

		now := utcNow()
		staleBefore := now.Add(-minAge)
		orphaned, err := selectOrphanedQueuedTasks(ctx, tx, staleBefore)
		if err != nil {
			return nil, err
		}
		return reconcileAndRecover(ctx, tx, orphaned, resetGhostTasks, "orphaned queued reconciliation")
	})
}

func reconcileAndRecover(ctx context.Context, tx *immediateTx, ghosts []models.Task, reset func(context.Context, *immediateTx, []models.Task, []string, time.Time) error, commitMsg string) ([]models.Task, error) {
	if len(ghosts) == 0 {
		return nil, commitTx(tx, commitMsg)
	}
	ids := make([]string, 0, len(ghosts))
	for _, task := range ghosts {
		ids = append(ids, task.ID)
	}
	now := utcNow()
	if err := reset(ctx, tx, ghosts, ids, now); err != nil {
		return nil, err
	}
	recovered, err := selectTasksByIDs(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	return recovered, commitTx(tx, commitMsg)
}
