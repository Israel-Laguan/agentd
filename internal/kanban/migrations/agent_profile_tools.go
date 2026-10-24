package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateToV25 adds the tool allowlist and the read-only workspace flag to
// agent_profiles. allowed_tools holds a JSON array of tool names, and "[]"
// means no allowlist, so existing profiles stay unrestricted. workspace_readonly
// is stored here for the read-only workspace mount that reads it later.
func migrateToV25(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "agent_profiles")
	if err != nil {
		return fmt.Errorf("check agent_profiles table for schema migration v25: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 25)
	}
	columns := []struct {
		name string
		ddl  string
	}{
		{"allowed_tools", `ALTER TABLE agent_profiles ADD COLUMN allowed_tools TEXT NOT NULL DEFAULT '[]'`},
		{"workspace_readonly", `ALTER TABLE agent_profiles ADD COLUMN workspace_readonly INTEGER NOT NULL DEFAULT 0 CHECK (workspace_readonly IN (0, 1))`},
	}
	for _, column := range columns {
		has, err := tableHasColumn(ctx, db, "agent_profiles", column.name)
		if err != nil {
			return fmt.Errorf("check agent_profiles.%s column: %w", column.name, err)
		}
		if has {
			continue
		}
		if _, err := db.ExecContext(ctx, column.ddl); err != nil {
			return fmt.Errorf("add agent_profiles.%s column: %w", column.name, err)
		}
	}
	return setSchemaVersion(ctx, db, 25)
}
