package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

const scheduledTasksTableSQL = `
CREATE TABLE IF NOT EXISTS scheduled_tasks (
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

func migrateToV12(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, scheduledTasksTableSQL); err != nil {
		return fmt.Errorf("create scheduled_tasks table: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_run_after ON scheduled_tasks(run_after)
		WHERE run_after IS NOT NULL AND run_after != ''`); err != nil {
		return fmt.Errorf("create scheduled_tasks run_after index: %w", err)
	}
	return setSchemaVersion(ctx, db, 12)
}
