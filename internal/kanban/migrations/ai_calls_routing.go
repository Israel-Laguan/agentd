package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateToV27 adds the routing decision to ai_calls: the purpose's required
// level and the rule the router matched. Rows written before it stay empty. A
// fresh database already has both from schema.sql, so each add is guarded.
func migrateToV27(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "ai_calls")
	if err != nil {
		return fmt.Errorf("check ai_calls table for schema migration v27: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 27)
	}
	for _, col := range []string{"required_level", "route_rule"} {
		has, err := tableHasColumn(ctx, db, "ai_calls", col)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := db.ExecContext(ctx, `ALTER TABLE ai_calls ADD COLUMN `+col+` TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add ai_calls.%s: %w", col, err)
		}
	}
	return setSchemaVersion(ctx, db, 27)
}
