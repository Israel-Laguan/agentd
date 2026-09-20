package db

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

// CompleteTieredOriginState resolves a tiered pipeline origin straight from
// BLOCKED, READY, or RUNNING to COMPLETED (success) or FAILED in a single
// UPDATE, guarded by the same optimistic-concurrency check (updated_at) as
// the normal result path.
//
// A tiered origin spends the whole pipeline BLOCKED, but UpdateTaskResult
// only accepts RUNNING tasks — so the old worker code walked the legal
// BLOCKED→READY→RUNNING ladder first. Each intermediate write committed
// separately, exposing a transient READY row that another dispatcher could
// claim via ClaimNextReadyTasks and re-block with a stale conflict verdict
// (SP-006). This single statement has no claimable intermediate state:
// either the row flips to terminal with the caller's version, or the write
// affects zero rows and reports ErrStateConflict.
//
// The generic UpdateTaskState machine intentionally still rejects
// BLOCKED→COMPLETED: only this narrowly-scoped origin path may bypass it.
func CompleteTieredOriginState(
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
		WHERE id = ? AND updated_at = ?
		  AND state IN (?, ?, ?)`,
		next, FormatTime(now), FormatTime(now),
		id, FormatTime(expectedUpdatedAt),
		models.TaskStateBlocked, models.TaskStateReady, models.TaskStateRunning)
	if err != nil {
		return fmt.Errorf("complete tiered origin: %w", err)
	}
	return RequireRowsAffected(update, 1, models.ErrStateConflict)
}
