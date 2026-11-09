package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV29(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return err
	}
	if !exists {
		return setSchemaVersion(ctx, db, 29)
	}
	has, err := tableHasColumn(ctx, db, "tasks", "golden_criteria")
	if err != nil {
		return err
	}
	if !has {
		_, err = db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN golden_criteria TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(golden_criteria) AND json_type(golden_criteria) = 'array')`)
		if err != nil {
			return fmt.Errorf("add golden criteria: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 29)
}
