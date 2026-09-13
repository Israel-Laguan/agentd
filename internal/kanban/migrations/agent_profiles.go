package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV7(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "agent_profiles")
	if err != nil {
		return fmt.Errorf("check agent_profiles table for schema migration v7: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 7)
	}
	additions := []struct {
		name string
		ddl  string
	}{
		{"role", `ALTER TABLE agent_profiles ADD COLUMN role TEXT NOT NULL DEFAULT 'CODE_GEN'`},
		{"max_tokens", `ALTER TABLE agent_profiles ADD COLUMN max_tokens INTEGER NOT NULL DEFAULT 0`},
	}
	for _, col := range additions {
		has, err := tableHasColumn(ctx, db, "agent_profiles", col.name)
		if err != nil {
			return fmt.Errorf("check agent_profiles.%s column: %w", col.name, err)
		}
		if has {
			continue
		}
		if _, err := db.ExecContext(ctx, col.ddl); err != nil {
			return fmt.Errorf("add agent_profiles.%s column: %w", col.name, err)
		}
	}
	return setSchemaVersion(ctx, db, 7)
}

func migrateToV9(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "agent_profiles")
	if err != nil {
		return fmt.Errorf("check agent_profiles table for schema migration v9: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 9)
	}
	has, err := tableHasColumn(ctx, db, "agent_profiles", "agentic_mode")
	if err != nil {
		return fmt.Errorf("check agent_profiles.agentic_mode column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE agent_profiles ADD COLUMN agentic_mode INTEGER NOT NULL DEFAULT 0 CHECK (agentic_mode IN (0, 1))`); err != nil {
			return fmt.Errorf("add agent_profiles.agentic_mode column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 9)
}
