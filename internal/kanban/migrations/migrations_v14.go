package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV14(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("check tasks table: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 14)
	}
	has, err := tableHasColumn(ctx, db, "tasks", "criteria_met")
	if err != nil {
		return fmt.Errorf("check tasks.criteria_met column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN criteria_met TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return fmt.Errorf("add tasks.criteria_met column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 14)
}
