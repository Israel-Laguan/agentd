package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV10(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "agent_profiles")
	if err != nil {
		return fmt.Errorf("check agent_profiles table for schema migration v10: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 10)
	}
	has, err := tableHasColumn(ctx, db, "agent_profiles", "disable_topic_drift")
	if err != nil {
		return fmt.Errorf("check agent_profiles.disable_topic_drift column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE agent_profiles ADD COLUMN disable_topic_drift INTEGER NOT NULL DEFAULT 0 CHECK (disable_topic_drift IN (0, 1))`); err != nil {
			return fmt.Errorf("add agent_profiles.disable_topic_drift column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 10)
}
