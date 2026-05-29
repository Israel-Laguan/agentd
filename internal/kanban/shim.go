// Package kanban - compatibility shim.
//
// This file provides type aliases, function-variable forwarding, and constant
// re-exports so that root-package files (and their tests) continue to compile
// after the utility code was extracted to kanban/db and kanban/domain.
package kanban

import (
	"context"
	"database/sql"
	"time"

	kdb "agentd/internal/kanban/db"
	"agentd/internal/kanban/domain"
)

// ---------------------------------------------------------------------------
// Constants re-exported from kanban/db (tests reference them directly).
// ---------------------------------------------------------------------------

const (
	sqliteBusy        = 5
	maxBusyRetries    = 6
	initialBackoff    = 5 * time.Millisecond
	backoffMultiplier = 2
)

// ---------------------------------------------------------------------------
// Type aliases — zero-cost: all callers see the exact same concrete type.
// ---------------------------------------------------------------------------

type immediateTx = kdb.ImmediateTx
type sqlExecutor = kdb.SQLExecutor
type sqlQueryer = kdb.SQLQueryer
type scanner = kdb.Scanner

// ---------------------------------------------------------------------------
// Function forwards — all non-generic helpers.
// ---------------------------------------------------------------------------

var (
	// time helpers
	parseTime  = kdb.ParseTime
	formatTime = kdb.FormatTime
	utcNow     = kdb.UTCNow

	// row helpers
	closeRows = kdb.CloseRows

	// sql helpers
	requireRowsAffected = kdb.RequireRowsAffected
	placeholders        = kdb.Placeholders
	nullString          = kdb.NullString

	// tx helpers
	beginImmediate          = kdb.BeginImmediate
	rollbackUnlessCommitted = kdb.RollbackUnlessCommitted
	commitTx                = kdb.CommitTx

	// scan helpers
	scanProject  = kdb.ScanProject
	scanProjects = kdb.ScanProjects
	scanTask     = kdb.ScanTask
	scanTasks    = kdb.ScanTasks
	scanSettings = kdb.ScanSettings

	// task select / query helpers
	taskSelectColumns          = kdb.TaskSelectColumns
	selectTaskSQL              = kdb.SelectTaskSQL
	selectReadyTaskIDs         = kdb.SelectReadyTaskIDs
	selectTaskByID             = kdb.SelectTaskByID
	selectTasksByIDs           = kdb.SelectTasksByIDs
	selectGhostTasks           = kdb.SelectGhostTasks
	selectStaleTasks           = kdb.SelectStaleTasks
	selectOrphanedQueuedTasks = kdb.SelectOrphanedQueuedTasks

	// state update helpers
	updateTaskStateInTx          = kdb.UpdateTaskStateInTx
	finishTaskStateSideEffects   = kdb.FinishTaskStateSideEffects
	queueReadyTaskIDs            = kdb.QueueReadyTaskIDs
	updateTaskResultState        = kdb.UpdateTaskResultState
	finishTaskResultSideEffects  = kdb.FinishTaskResultSideEffects
	resetGhostTasks              = kdb.ResetGhostTasks

	// materialization helpers
	insertTask         = kdb.InsertTask
	insertRelations    = kdb.InsertRelations
	insertTaskRelation = kdb.InsertTaskRelation

	// HITL / SQL fragment helpers
	selfHealingHandoffExcludeSQL = kdb.SelfHealingHandoffExcludeSQL
)

// ---------------------------------------------------------------------------
// Generic wrappers — Go generics cannot be aliased via var; thin wrappers are
// the correct forwarding mechanism.
// ---------------------------------------------------------------------------

func retryOnBusy[T any](ctx context.Context, op func(context.Context) (T, error)) (T, error) {
	return kdb.RetryOnBusy(ctx, op)
}

func retryOnBusyNoResult(ctx context.Context, op func(context.Context) error) error {
	return kdb.RetryOnBusyNoResult(ctx, op)
}

// Open opens the SQLite database at path and configures the connection pool.
func Open(path string) (*sql.DB, error) { return kdb.Open(path) }

// ---------------------------------------------------------------------------
// ensureNoCycle forwards to domain.EnsureNoCycle.
// ---------------------------------------------------------------------------

func ensureNoCycle(ctx context.Context, q sqlQueryer, parentID, childID string) error {
	return domain.EnsureNoCycle(ctx, q, parentID, childID)
}
