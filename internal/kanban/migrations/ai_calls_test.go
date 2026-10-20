package migrations

import (
	"context"
	"database/sql"
	"testing"
)

func aiCallsColumns(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(ai_calls)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, name)
	}
	return cols
}

// TestMigrateToV21_CreatesAICallsOnAnUpgrade: a v20 database has no ai_calls
// table; the migration adds it with the indexes the report reads, and a second
// run changes nothing.
func TestMigrateToV21_CreatesAICallsOnAnUpgrade(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v21-upgrade?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v18SchemaWithoutUserIDSQL); err != nil {
		t.Fatalf("create v18 schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := setSchemaVersion(ctx, db, 20); err != nil {
		t.Fatalf("seed schema version: %v", err)
	}
	if got := aiCallsColumns(t, db); len(got) != 0 {
		t.Fatalf("ai_calls exists before the migration: %v", got)
	}

	if err := Run(ctx, db, t.TempDir()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertAICallsShape(t, db, "context_mix", "truncated_by")
	if err := Run(ctx, db, t.TempDir()); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	assertSchemaVersion(t, db, ctx, "23")
}

func assertAICallsShape(t *testing.T, db *sql.DB, extra ...string) {
	t.Helper()
	want := []string{"id", "at", "kind", "purpose", "stage", "role", "task_id", "agent_id", "correlation_id",
		"provider", "model", "attempt", "cascade", "outcome", "truncated", "message_count", "tool_count",
		"input_chars", "output_chars", "tokens", "cached_tokens", "cache_write_tokens", "latency_ms"}
	want = append(want, extra...)
	got := aiCallsColumns(t, db)
	if len(got) != len(want) {
		t.Fatalf("ai_calls columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ai_calls column %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, idx := range []string{"idx_ai_calls_at_purpose", "idx_ai_calls_task", "idx_ai_calls_correlation"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Fatalf("index %s missing: %v", idx, err)
		}
	}
}

// TestMigrateToV21_FreshDatabase: the migration is idempotent when the table
// already exists (a fresh database gets it from schema.sql first).
func TestMigrateToV21_FreshDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v21-fresh?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := migrateToV21(ctx, db); err != nil {
			t.Fatalf("migrateToV21 run %d: %v", i, err)
		}
	}
	assertAICallsShape(t, db)
	assertSchemaVersion(t, db, ctx, "21")
}
