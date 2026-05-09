package kanban

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
)

func queueReadyTaskIDs(ctx context.Context, tx *immediateTx, ids []string, now time.Time) error {
	args := []any{models.TaskStateQueued, formatTime(now), models.TaskStateReady}
	args = append(args, taskIDsAsAny(ids)...)
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, updated_at = ?
		WHERE state = ? AND id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return fmt.Errorf("queue ready tasks: %w", err)
	}
	return requireRowsAffected(result, int64(len(ids)), models.ErrStateConflict)
}

func updateTaskResultState(
	ctx context.Context,
	tx *immediateTx,
	id string,
	expectedUpdatedAt time.Time,
	success bool,
	now time.Time,
) error {
	next := models.TaskStateFailed
	if success {
		next = models.TaskStateCompleted
	}
	update, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND updated_at = ? AND state = ?`,
		next, formatTime(now), formatTime(now), id, formatTime(expectedUpdatedAt), models.TaskStateRunning)
	if err != nil {
		return fmt.Errorf("update task result: %w", err)
	}
	return requireRowsAffected(update, 1, models.ErrStateConflict)
}

func finishTaskResultSideEffects(ctx context.Context, tx *immediateTx, id string, result models.TaskResult, now time.Time) error {
	if result.Success {
		if err := unlockReadyChildren(ctx, tx, id, now); err != nil {
			return err
		}
		if err := unblockReadyParents(ctx, tx, id, now); err != nil {
			return err
		}
	}
	if strings.TrimSpace(result.Payload) == "" {
		return nil
	}
	return appendTaskResultEvent(ctx, tx, id, result.Payload, now)
}

func unblockReadyParents(ctx context.Context, tx *immediateTx, childID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
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
		      AND child.state != ?
		  )`,
		models.TaskStateReady, formatTime(now), models.TaskStateBlocked, childID, models.TaskStateCompleted)
	if err != nil {
		return fmt.Errorf("unblock completed task parents: %w", err)
	}
	return nil
}

func resetGhostTasks(ctx context.Context, tx *immediateTx, ghosts []models.Task, ids []string, now time.Time) error {
	args := []any{models.TaskStateReady, formatTime(now)}
	args = append(args, taskIDsAsAny(ids)...)
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, os_process_id = NULL, last_heartbeat = NULL, completed_at = NULL, updated_at = ?
		WHERE id IN (`+placeholders(len(ids))+`)`, args...); err != nil {
		return fmt.Errorf("reset ghost tasks: %w", err)
	}
	for _, task := range ghosts {
		if err := appendRecoveryEvent(ctx, tx, task, now); err != nil {
			return err
		}
	}
	return nil
}

func commitTx(tx *immediateTx, label string) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", label, err)
	}
	return nil
}
