package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"

	"github.com/google/uuid"
)

func selectTaskSQL() string {
	return `
		SELECT id, project_id, agent_id, title, description, state, assignee,
		       os_process_id, started_at, completed_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
		FROM tasks`
}

func selectReadyTaskIDs(ctx context.Context, q sqlQueryer, limit int) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id
		FROM tasks
		WHERE state = ? AND assignee = ?
		ORDER BY created_at
		LIMIT ?`, models.TaskStateReady, models.TaskAssigneeSystem, limit)
	if err != nil {
		return nil, fmt.Errorf("select ready task ids: %w", err)
	}
	defer closeRows(rows)

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan ready task id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ready task ids: %w", err)
	}
	return ids, nil
}

func selectTaskByID(ctx context.Context, q sqlQueryer, id string) (*models.Task, error) {
	tasks, err := selectTasksByIDs(ctx, q, []string{id})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, models.ErrTaskNotFound
	}
	return &tasks[0], nil
}

func selectTasksByIDs(ctx context.Context, q sqlQueryer, ids []string) ([]models.Task, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(
		ctx,
		selectTaskSQL()+" WHERE id IN ("+placeholders(len(ids))+") ORDER BY created_at",
		taskIDsAsAny(ids)...,
	)
	if err != nil {
		return nil, fmt.Errorf("select tasks by ids: %w", err)
	}
	defer closeRows(rows)
	return scanTasks(rows)
}

func unlockReadyChildren(ctx context.Context, tx *immediateTx, parentID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, updated_at = ?
		WHERE state = ?
		  AND id IN (
		    SELECT child_task_id
		    FROM task_relations
		    WHERE parent_task_id = ?
		  )
		  AND NOT EXISTS (
		    SELECT 1
		    FROM task_relations tr
		    JOIN tasks parent ON parent.id = tr.parent_task_id
		    WHERE tr.child_task_id = tasks.id
		      AND parent.state != ?
		  )`,
		models.TaskStateReady, formatTime(now), models.TaskStatePending, parentID, models.TaskStateCompleted)
	if err != nil {
		return fmt.Errorf("unlock completed task children: %w", err)
	}
	return nil
}

func appendTaskResultEvent(ctx context.Context, tx *immediateTx, taskID, payload string, now time.Time) error {
	task, err := selectTaskByID(ctx, tx, taskID)
	if err != nil {
		return err
	}
	eventType := models.EventTypeResult
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), task.ProjectID, taskID, eventType, payload, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("append task result event: %w", err)
	}
	return nil
}

func selectGhostTasks(ctx context.Context, q sqlQueryer, alivePIDs []int) ([]models.Task, error) {
	query := selectTaskSQL() + `
		WHERE state = ? AND os_process_id IS NOT NULL`
	args := []any{models.TaskStateRunning}
	if len(alivePIDs) > 0 {
		query += " AND os_process_id NOT IN (" + placeholders(len(alivePIDs)) + ")"
		for _, pid := range alivePIDs {
			args = append(args, pid)
		}
	}
	query += " ORDER BY created_at"

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select ghost tasks: %w", err)
	}
	defer closeRows(rows)
	return scanTasks(rows)
}

func selectOrphanedQueuedTasks(ctx context.Context, q sqlQueryer, staleBefore time.Time) ([]models.Task, error) {
	query := selectTaskSQL() + `
		WHERE state = ? AND started_at IS NULL AND updated_at < ?
		ORDER BY created_at`
	rows, err := q.QueryContext(ctx, query, models.TaskStateQueued, formatTime(staleBefore))
	if err != nil {
		return nil, fmt.Errorf("select orphaned queued tasks: %w", err)
	}
	defer closeRows(rows)
	return scanTasks(rows)
}

func selectStaleTasks(ctx context.Context, q sqlQueryer, alivePIDs []int, staleBefore time.Time) ([]models.Task, error) {
	query := selectTaskSQL() + `
		WHERE state = ? AND (
			last_heartbeat IS NULL OR last_heartbeat < ?`
	args := []any{models.TaskStateRunning, formatTime(staleBefore)}
	if len(alivePIDs) > 0 {
		query += " OR os_process_id IS NULL OR os_process_id NOT IN (" + placeholders(len(alivePIDs)) + ")"
		for _, pid := range alivePIDs {
			args = append(args, pid)
		}
	} else {
		query += " OR os_process_id IS NOT NULL"
	}
	query += ") ORDER BY created_at"

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select stale tasks: %w", err)
	}
	defer closeRows(rows)
	return scanTasks(rows)
}

func appendRecoveryEvent(ctx context.Context, tx sqlExecutor, task models.Task, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), task.ProjectID, task.ID, models.EventTypeRecovery, "reset ghost task to READY", formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("append recovery event: %w", err)
	}
	return nil
}
