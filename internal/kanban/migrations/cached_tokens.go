package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV15(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("check tasks table: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 15)
	}
	hasCached, err := tableHasColumn(ctx, db, "tasks", "cached_token_usage")
	if err != nil {
		return fmt.Errorf("check tasks.cached_token_usage column: %w", err)
	}
	if !hasCached {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN cached_token_usage INTEGER NOT NULL DEFAULT 0 CHECK (cached_token_usage >= 0)`); err != nil {
			return fmt.Errorf("add tasks.cached_token_usage column: %w", err)
		}
	}
	hasWrite, err := tableHasColumn(ctx, db, "tasks", "cache_write_token_usage")
	if err != nil {
		return fmt.Errorf("check tasks.cache_write_token_usage column: %w", err)
	}
	if !hasWrite {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN cache_write_token_usage INTEGER NOT NULL DEFAULT 0 CHECK (cache_write_token_usage >= 0)`); err != nil {
			return fmt.Errorf("add tasks.cache_write_token_usage column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 15)
}
