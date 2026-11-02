package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateToV28 adds gated_tools to agent_profiles: a JSON array of the tools
// whose calls wait for a human approval. "[]" gates nothing, so existing
// profiles run as before. A fresh database already has it from schema.sql.
func migrateToV28(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "agent_profiles")
	if err != nil {
		return fmt.Errorf("check agent_profiles table for schema migration v28: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 28)
	}
	has, err := tableHasColumn(ctx, db, "agent_profiles", "gated_tools")
	if err != nil {
		return fmt.Errorf("check agent_profiles.gated_tools column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE agent_profiles ADD COLUMN gated_tools TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return fmt.Errorf("add agent_profiles.gated_tools column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 28)
}
