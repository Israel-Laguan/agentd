package migrations

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
)

// TestMigrateToV23_CreatesToolCallsAndIsIdempotent: a v22 database gains the
// tool_calls table with the index the repeated-reads query uses.
func TestMigrateToV23_CreatesToolCallsAndIsIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v23?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for _, stmt := range []string{v18SchemaWithoutUserIDSQL,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed schema: %v", err)
		}
	}
	if err := setSchemaVersion(ctx, db, 22); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Run(ctx, db, t.TempDir()); err != nil {
			t.Fatalf("Run #%d: %v", i+1, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tool_calls (at, task_id, call_id, tool, path, status, result_bytes)
		VALUES ('2026-10-20T00:00:00Z', 't', 'c', 'read', 'a.go', 'ok', 5)`); err != nil {
		t.Fatalf("insert into tool_calls: %v", err)
	}
	var idx string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_tool_calls_task_path'`).Scan(&idx); err != nil {
		t.Fatalf("index missing: %v", err)
	}
	assertSchemaVersion(t, db, ctx, strconv.Itoa(currentSchemaVersion))
}
