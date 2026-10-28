package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateToV26 adds tasks.requires_workspace_change. 0 means a text-only finish
// may complete the task, so existing tasks keep their behavior.
func migrateToV26(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("check tasks table for schema migration v26: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 26)
	}
	has, err := tableHasColumn(ctx, db, "tasks", "requires_workspace_change")
	if err != nil {
		return fmt.Errorf("check tasks.requires_workspace_change column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN requires_workspace_change INTEGER NOT NULL DEFAULT 0 CHECK (requires_workspace_change IN (0, 1))`); err != nil {
			return fmt.Errorf("add tasks.requires_workspace_change column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 26)
}
