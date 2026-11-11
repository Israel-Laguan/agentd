package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateToV30 adds tasks.requires_workspace_change_classified. 0 means the
// task's requires_workspace_change value was not explicitly classified, so the
// worker keeps write access; 1 means it was, and false is a text-only task.
func migrateToV30(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("check tasks table: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 30)
	}
	has, err := tableHasColumn(ctx, db, "tasks", "requires_workspace_change_classified")
	if err != nil {
		return fmt.Errorf("check tasks.requires_workspace_change_classified column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN requires_workspace_change_classified INTEGER NOT NULL DEFAULT 0 CHECK (requires_workspace_change_classified IN (0, 1))`); err != nil {
			return fmt.Errorf("add tasks.requires_workspace_change_classified column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 30)
}
