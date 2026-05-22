package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateToV13CreatesStrictScheduledTasks(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v13?mode=memory&cache=shared")
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
	if version != "13" {
		t.Fatalf("schema version = %q, want 13", version)
	}

	var createSQL string
	if err := db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'scheduled_tasks'`).Scan(&createSQL); err != nil {
		t.Fatalf("read scheduled_tasks ddl: %v", err)
	}
	if !strings.Contains(strings.ToUpper(createSQL), "STRICT") {
		t.Fatalf("scheduled_tasks ddl missing STRICT: %s", createSQL)
	}
}

func TestMigrateToV13RebuildsNonStrictTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v13-rebuild?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v11SchemaSQL); err != nil {
		t.Fatalf("create v11 schema: %v", err)
	}
	nonStrictSQL := strings.Replace(scheduledTasksTableSQL, ") STRICT", ")", 1)
	if _, err := db.ExecContext(ctx, nonStrictSQL); err != nil {
		t.Fatalf("precreate non-strict scheduled_tasks: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE settings SET value = '12' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("set schema version 12: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scheduled_tasks (id, cron_expr, created_at, updated_at)
		VALUES ('keep-me', '*/5 * * * *', '2026-05-21T10:00:00Z', '2026-05-21T10:00:00Z')`); err != nil {
		t.Fatalf("seed scheduled_tasks: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "13" {
		t.Fatalf("schema version = %q, want 13", version)
	}

	var createSQL string
	if err := db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'scheduled_tasks'`).Scan(&createSQL); err != nil {
		t.Fatalf("read scheduled_tasks ddl: %v", err)
	}
	if !strings.Contains(strings.ToUpper(createSQL), "STRICT") {
		t.Fatalf("scheduled_tasks ddl missing STRICT: %s", createSQL)
	}

	var id string
	if err := db.QueryRowContext(ctx, `SELECT id FROM scheduled_tasks WHERE id = 'keep-me'`).Scan(&id); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if id != "keep-me" {
		t.Fatalf("id = %q, want keep-me", id)
	}

	var idxCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='index' AND name='idx_scheduled_tasks_run_after'`).Scan(&idxCount); err != nil {
		t.Fatalf("check scheduled_tasks index: %v", err)
	}
	if idxCount != 1 {
		t.Fatalf("idx_scheduled_tasks_run_after exists = %d, want 1", idxCount)
	}
}
