package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const scheduledTasksStrictNewSQL = `
CREATE TABLE scheduled_tasks_new (
	id TEXT PRIMARY KEY,
	cron_expr TEXT NOT NULL DEFAULT '',
	run_after TEXT,
	task_type TEXT NOT NULL DEFAULT '',
	context_fn TEXT NOT NULL DEFAULT '',
	context_args TEXT NOT NULL DEFAULT '{}',
	output_target TEXT NOT NULL DEFAULT 'LOG',
	title TEXT NOT NULL DEFAULT '',
	description_template TEXT NOT NULL DEFAULT '',
	project_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT 'dispatch' CHECK (kind IN ('dispatch', 'requeue')),
	target_task_id TEXT NOT NULL DEFAULT '',
	last_fired_at TEXT,
	enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
) STRICT`

const copyScheduledTasksToStrictSQL = `
INSERT INTO scheduled_tasks_new (
	id, cron_expr, run_after, task_type, context_fn, context_args, output_target,
	title, description_template, project_id, kind, target_task_id, last_fired_at,
	enabled, created_at, updated_at
)
SELECT
	id, cron_expr, run_after, task_type, context_fn, context_args, output_target,
	title, description_template, project_id, kind, target_task_id, last_fired_at,
	enabled, created_at, updated_at
FROM scheduled_tasks`

const scheduledTasksRunAfterIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_run_after ON scheduled_tasks(run_after)
WHERE run_after IS NOT NULL AND run_after != ''`

func migrateToV13(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "scheduled_tasks")
	if err != nil {
		return fmt.Errorf("check scheduled_tasks table: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 13)
	}

	createSQL, err := readTableSQL(ctx, db, "scheduled_tasks")
	if err != nil {
		return fmt.Errorf("read scheduled_tasks schema: %w", err)
	}
	if strings.Contains(strings.ToUpper(createSQL), "STRICT") {
		return setSchemaVersion(ctx, db, 13)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration v13: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS scheduled_tasks_new`); err != nil {
		return fmt.Errorf("drop stale scheduled_tasks_new: %w", err)
	}
	if _, err := tx.ExecContext(ctx, scheduledTasksStrictNewSQL); err != nil {
		return fmt.Errorf("create scheduled_tasks strict: %w", err)
	}
	if _, err := tx.ExecContext(ctx, copyScheduledTasksToStrictSQL); err != nil {
		return fmt.Errorf("copy scheduled_tasks strict: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE scheduled_tasks`); err != nil {
		return fmt.Errorf("drop old scheduled_tasks table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE scheduled_tasks_new RENAME TO scheduled_tasks`); err != nil {
		return fmt.Errorf("rename scheduled_tasks strict: %w", err)
	}
	if _, err := tx.ExecContext(ctx, scheduledTasksRunAfterIndexSQL); err != nil {
		return fmt.Errorf("recreate scheduled_tasks run_after index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration v13: %w", err)
	}
	return setSchemaVersion(ctx, db, 13)
}
