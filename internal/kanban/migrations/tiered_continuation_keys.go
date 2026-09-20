package migrations

import (
	"context"
	"database/sql"
)

func migrateToV17(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS tiered_continuation_keys (
		origin_id TEXT NOT NULL,
		idempotency_key TEXT NOT NULL,
		child_ids TEXT NOT NULL,
		PRIMARY KEY (origin_id, idempotency_key)
	) STRICT`); err != nil {
		return err
	}
	return setSchemaVersion(ctx, db, 17)
}
