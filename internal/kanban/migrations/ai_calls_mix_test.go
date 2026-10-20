package migrations

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigrateToV22_AddsMixColumnsToExistingRows: a v21 database keeps its rows
// and gains context_mix and truncated_by with empty defaults; running again
// changes nothing.
func TestMigrateToV22_AddsMixColumnsToExistingRows(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v22-upgrade?mode=memory&cache=shared")
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
	var mix, by string
	if err := db.QueryRow(`SELECT context_mix, truncated_by FROM ai_calls`).Scan(&mix, &by); err != nil {
		t.Fatalf("read new columns: %v", err)
	}
	if mix != "" || by != "" {
		t.Fatalf("defaults = %q, %q, want empty", mix, by)
	}
	assertSchemaVersion(t, db, ctx, "22")
}
