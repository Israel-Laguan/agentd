package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV5(ctx context.Context, db *sql.DB) error {
	hasCompletedAt, err := tableHasColumn(ctx, db, "tasks", "completed_at")
	if err != nil {
		return fmt.Errorf("check tasks.completed_at column: %w", err)
	}
	if !hasCompletedAt {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN completed_at TEXT`); err != nil {
			return fmt.Errorf("add tasks.completed_at column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 5)
}
