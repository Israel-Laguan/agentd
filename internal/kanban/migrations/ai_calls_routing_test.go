package migrations

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
)

// TestMigrateToV27_AddsRoutingColumnsToExistingRows: a v21 database keeps its
// rows and gains required_level and route_rule with empty defaults; running
// again changes nothing.
func TestMigrateToV27_AddsRoutingColumnsToExistingRows(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v27-upgrade?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for _, stmt := range append([]string{v18SchemaWithoutUserIDSQL,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`}, aiCallsDDL...) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed v21 schema: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO ai_calls (at, kind, purpose, stage, provider, attempt, outcome)
		VALUES ('2026-10-19T00:00:00Z', 'generate', 'exec.turn', 'exec', 'p', 'first', 'ok')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := setSchemaVersion(ctx, db, 21); err != nil {
		t.Fatalf("seed schema version: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := Run(ctx, db, t.TempDir()); err != nil {
			t.Fatalf("Run #%d: %v", i+1, err)
		}
	}
	var level, rule string
	if err := db.QueryRow(`SELECT required_level, route_rule FROM ai_calls`).Scan(&level, &rule); err != nil {
		t.Fatalf("read new columns: %v", err)
	}
	if level != "" || rule != "" {
		t.Fatalf("defaults = %q, %q, want empty", level, rule)
	}
	assertSchemaVersion(t, db, ctx, strconv.Itoa(currentSchemaVersion))
}
