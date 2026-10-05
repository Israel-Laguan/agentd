package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateToV20 adds projects.started_empty: whether the project's workspace was
// created empty and never seeded.
//
// The opt-in recovery reset (recovery.clean_workspace_on_recover, B-010) must
// restore a recovered task's workspace to the project's starting state. A
// source_path seed or a hand-populated workspace is not recorded anywhere, so
// the only state agentd can restore is "empty". Existing projects default to 0:
// their starting state is unknown, so the reset refuses them instead of deleting
// content it cannot put back.
func migrateToV20(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "projects")
	if err != nil {
		return fmt.Errorf("check projects table: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 20)
	}
	has, err := tableHasColumn(ctx, db, "projects", "started_empty")
	if err != nil {
		return fmt.Errorf("check projects.started_empty column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE projects ADD COLUMN started_empty INTEGER NOT NULL DEFAULT 0 CHECK (started_empty IN (0, 1))`); err != nil {
			return fmt.Errorf("add projects.started_empty column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 20)
}
