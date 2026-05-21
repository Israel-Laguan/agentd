package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV11(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "agent_profiles")
	if err != nil {
		return fmt.Errorf("check agent_profiles table for schema migration v11: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 11)
	}
	has, err := tableHasColumn(ctx, db, "agent_profiles", "capability_route_intent")
	if err != nil {
		return fmt.Errorf("check agent_profiles.capability_route_intent column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE agent_profiles ADD COLUMN capability_route_intent TEXT`); err != nil {
			return fmt.Errorf("add agent_profiles.capability_route_intent column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 11)
}
