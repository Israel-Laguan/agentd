package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// toolCallsDDL mirrors the tool_calls block in kanban/db/schema.sql.
var toolCallsDDL = []string{`
CREATE TABLE IF NOT EXISTS tool_calls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	at TEXT NOT NULL,
	task_id TEXT NOT NULL,
	call_id TEXT NOT NULL DEFAULT '',
	tool TEXT NOT NULL,
	path TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	result_bytes INTEGER NOT NULL DEFAULT 0 CHECK (result_bytes >= 0)
) STRICT`,
	`CREATE INDEX IF NOT EXISTS idx_tool_calls_task_path ON tool_calls(task_id, path)`,
}

// migrateToV23 adds tool_calls: one row per tool dispatch, so tool use and
// repeated reads survive the deletion of curated events (T-044).
func migrateToV23(ctx context.Context, db *sql.DB) error {
	for _, stmt := range toolCallsDDL {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create tool_calls: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 23)
}
