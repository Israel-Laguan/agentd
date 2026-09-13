package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV8(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("check tasks table for schema migration v8: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 8)
	}
	has, err := tableHasColumn(ctx, db, "tasks", "success_criteria")
	if err != nil {
		return fmt.Errorf("check tasks.success_criteria column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN success_criteria TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(success_criteria) AND json_type(success_criteria) = 'array')`); err != nil {
			return fmt.Errorf("add tasks.success_criteria column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 8)
}
