package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

// TaskSelectColumns returns a SELECT projection for the tasks table.
func TaskSelectColumns(table string) string {
	return fmt.Sprintf(`
		SELECT %s.id, %s.project_id, %s.agent_id, %s.title, %s.description, %s.state, %s.assignee,
		       %s.os_process_id, %s.started_at, %s.completed_at, %s.last_heartbeat, %s.retry_count, %s.token_usage, %s.success_criteria, %s.criteria_met, %s.created_at, %s.updated_at`,
		table, table, table, table, table, table, table,
		table, table, table, table, table, table, table, table, table, table)
}

// SelectTaskSQL returns the base SELECT query for tasks.
func SelectTaskSQL() string {
	return TaskSelectColumns("tasks") + `
		FROM tasks`
}

// SelectReadyTaskIDs returns READY tasks assigned to the system.
func SelectReadyTaskIDs(ctx context.Context, q SQLQueryer, limit int) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id
		FROM tasks
		WHERE state = ? AND assignee = ?
		ORDER BY created_at
		LIMIT ?`, models.TaskStateReady, models.TaskAssigneeSystem, limit)
	if err != nil {
		return nil, fmt.Errorf("select ready task ids: %w", err)
	}
	defer CloseRows(rows)

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

// SelectTaskByID returns one task by ID.
func SelectTaskByID(ctx context.Context, q SQLQueryer, id string) (*models.Task, error) {
	tasks, err := SelectTasksByIDs(ctx, q, []string{id})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, models.ErrTaskNotFound
	}
	return &tasks[0], nil
}

// SelectTasksByIDs returns tasks for the given IDs in created_at order.
func SelectTasksByIDs(ctx context.Context, q SQLQueryer, ids []string) ([]models.Task, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(
		ctx,
		SelectTaskSQL()+" WHERE id IN ("+Placeholders(len(ids))+") ORDER BY created_at",
		TaskIDsAsAny(ids)...,
	)
	if err != nil {
		return nil, fmt.Errorf("select tasks by ids: %w", err)
	}
	defer CloseRows(rows)
	return ScanTasks(rows)
}

// UnlockReadyChildren moves pending child tasks to READY when all parents are resolved.
func UnlockReadyChildren(ctx context.Context, tx *ImmediateTx, parentID string, now time.Time) error {
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
		models.TaskStateReady, FormatTime(now), models.TaskStatePending, parentID, models.TaskStateCompleted)
	if err != nil {
		return fmt.Errorf("unlock completed task children: %w", err)
	}
	return nil
}

// AppendTaskResultEvent writes a task result event.
func AppendTaskResultEvent(ctx context.Context, tx *ImmediateTx, taskID, payload string, now time.Time) error {
	task, err := SelectTaskByID(ctx, tx, taskID)
	if err != nil {
		return err
	}
	eventType := models.EventTypeResult
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), task.ProjectID, taskID, eventType, payload, FormatTime(now), FormatTime(now))
	if err != nil {
		return fmt.Errorf("append task result event: %w", err)
	}
	return nil
}

// SelectGhostTasks returns running tasks with dead PIDs.
func SelectGhostTasks(ctx context.Context, q SQLQueryer, alivePIDs []int) ([]models.Task, error) {
	query := SelectTaskSQL() + `
		WHERE state = ? AND os_process_id IS NOT NULL`
	args := []any{models.TaskStateRunning}
	if len(alivePIDs) > 0 {
		query += " AND os_process_id NOT IN (" + Placeholders(len(alivePIDs)) + ")"
		for _, pid := range alivePIDs {
			args = append(args, pid)
		}
	}
	query += " ORDER BY created_at"

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select ghost tasks: %w", err)
	}
	defer CloseRows(rows)
	return ScanTasks(rows)
}

// SelectOrphanedQueuedTasks returns queued tasks older than staleBefore.
func SelectOrphanedQueuedTasks(ctx context.Context, q SQLQueryer, staleBefore time.Time) ([]models.Task, error) {
	query := SelectTaskSQL() + `
		WHERE state = ? AND updated_at < ?
		ORDER BY created_at`
	rows, err := q.QueryContext(ctx, query, models.TaskStateQueued, FormatTime(staleBefore))
	if err != nil {
		return nil, fmt.Errorf("select orphaned queued tasks: %w", err)
	}
	defer CloseRows(rows)
	return ScanTasks(rows)
}

// SelectStaleTasks returns running tasks that have exceeded the heartbeat threshold.
func SelectStaleTasks(ctx context.Context, q SQLQueryer, alivePIDs []int, staleBefore time.Time) ([]models.Task, error) {
	query := SelectTaskSQL() + `
		WHERE state = ? AND (
			last_heartbeat IS NULL OR last_heartbeat < ?`
	args := []any{models.TaskStateRunning, FormatTime(staleBefore)}
	if len(alivePIDs) > 0 {
		query += " OR os_process_id IS NULL OR os_process_id NOT IN (" + Placeholders(len(alivePIDs)) + ")"
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
	defer CloseRows(rows)
	return ScanTasks(rows)
}

// AppendRecoveryEvent records a recovery event for a task.
func AppendRecoveryEvent(ctx context.Context, tx SQLExecutor, task models.Task, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), task.ProjectID, task.ID, models.EventTypeRecovery, "reset ghost task to READY", FormatTime(now), FormatTime(now))
	if err != nil {
		return fmt.Errorf("append recovery event: %w", err)
	}
	return nil
}

// UnblockBlockedParentsWhenChildrenResolved updates blocked parents whose children are now resolved.
func UnblockBlockedParentsWhenChildrenResolved(ctx context.Context, tx *ImmediateTx, childID string, now time.Time) error {
	resolvedSQL, resolvedArgs := ChildResolvedConditionSQL("child")
	args := []any{models.TaskStateReady, FormatTime(now), models.TaskStateBlocked, childID}
	args = append(args, resolvedArgs...)
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE tasks
		SET state = ?, updated_at = ?
		WHERE state = ?
		  AND id IN (
		    SELECT parent_task_id
		    FROM task_relations
		    WHERE child_task_id = ?
		  )
		  AND NOT EXISTS (
		    SELECT 1
		    FROM task_relations tr
		    JOIN tasks child ON child.id = tr.child_task_id
		    WHERE tr.parent_task_id = tasks.id
		      AND NOT %s
		  )`, resolvedSQL), args...)
	if err != nil {
		return fmt.Errorf("unblock resolved task parents: %w", err)
	}
	return nil
}
