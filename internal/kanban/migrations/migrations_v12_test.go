package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateToV12CreatesScheduledTasksTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v12?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v11SchemaSQL); err != nil {
		t.Fatalf("create v11 schema: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "12" {
		t.Fatalf("schema version = %q, want 12", version)
	}

	var tableExists int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'scheduled_tasks'`).Scan(&tableExists); err != nil {
		t.Fatalf("check scheduled_tasks table: %v", err)
	}
	if tableExists != 1 {
		t.Fatalf("scheduled_tasks table exists = %d, want 1", tableExists)
	}
}

const v11SchemaSQL = `
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '11', datetime('now'));
`
