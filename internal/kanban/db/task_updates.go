package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
)

// QueueReadyTaskIDs transitions a set of READY tasks to QUEUED inside a transaction.
func QueueReadyTaskIDs(ctx context.Context, tx *ImmediateTx, ids []string, now time.Time) error {
	args := []any{models.TaskStateQueued, FormatTime(now), models.TaskStateReady}
	args = append(args, TaskIDsAsAny(ids)...)
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, updated_at = ?
		WHERE state = ? AND id IN (`+Placeholders(len(ids))+`)`, args...)
	if err != nil {
		return fmt.Errorf("queue ready tasks: %w", err)
	}
	return RequireRowsAffected(result, int64(len(ids)), models.ErrStateConflict)
}

// UpdateTaskResultState marks a RUNNING task as COMPLETED or FAILED.
func UpdateTaskResultState(
	ctx context.Context,
	tx *ImmediateTx,
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
		next, FormatTime(now), FormatTime(now), id, FormatTime(expectedUpdatedAt), models.TaskStateRunning)
	if err != nil {
		return fmt.Errorf("update task result: %w", err)
	}
	return RequireRowsAffected(update, 1, models.ErrStateConflict)
}

// FinishTaskResultSideEffects applies post-result side effects: unlock
// children, unblock parents, and append a result event.
func FinishTaskResultSideEffects(ctx context.Context, tx *ImmediateTx, id string, result models.TaskResult, now time.Time) error {
	if result.Success {
		if err := UnlockReadyChildren(ctx, tx, id, now); err != nil {
			return err
		}
	}
	if err := UnblockBlockedParentsWhenChildrenResolved(ctx, tx, id, now); err != nil {
		return err
	}
	if strings.TrimSpace(result.Payload) == "" {
		return nil
	}
	return AppendTaskResultEvent(ctx, tx, id, result.Payload, now)
}

// ResetGhostTasks resets ghost/stale tasks back to READY and writes recovery events.
func ResetGhostTasks(ctx context.Context, tx *ImmediateTx, ghosts []models.Task, ids []string, now time.Time) error {
	args := []any{models.TaskStateReady, FormatTime(now)}
	args = append(args, TaskIDsAsAny(ids)...)
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, os_process_id = NULL, last_heartbeat = NULL, completed_at = NULL, updated_at = ?
		WHERE id IN (`+Placeholders(len(ids))+`)`, args...); err != nil {
		return fmt.Errorf("reset ghost tasks: %w", err)
	}
	for _, task := range ghosts {
		if err := AppendRecoveryEvent(ctx, tx, task, now); err != nil {
			return err
		}
	}
	return nil
}

// CommitTx commits the transaction, wrapping the error with the operation label.
func CommitTx(tx *ImmediateTx, label string) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", label, err)
	}
	return nil
}
