package migrations

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigrateToV20_AddsProjectStartedEmpty: existing projects must read as NOT
// started empty, so the opt-in recovery reset refuses them instead of deleting
// content whose starting state was never recorded.
func TestMigrateToV20_AddsProjectStartedEmpty(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v20-started-empty?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	projectsDir := t.TempDir()
	if _, err := db.ExecContext(ctx, v18SchemaWithoutUserIDSQL); err != nil {
		t.Fatalf("create v18 schema: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := setSchemaVersion(ctx, db, 18); err != nil {
		t.Fatalf("seed schema version: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		 VALUES ('p1', 'proj', 'in', ?, 'ACTIVE', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		projectsDir+"/p1"); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	if err := Run(ctx, db, projectsDir); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var startedEmpty int
	if err := db.QueryRowContext(ctx, `SELECT started_empty FROM projects WHERE id = 'p1'`).Scan(&startedEmpty); err != nil {
		t.Fatalf("read projects.started_empty: %v", err)
	}
	if startedEmpty != 0 {
		t.Fatalf("pre-existing project started_empty = %d, want 0", startedEmpty)
	}
	if _, err := db.ExecContext(ctx, `UPDATE projects SET started_empty = 2 WHERE id = 'p1'`); err == nil {
		t.Fatal("started_empty accepted 2; want a 0/1 check")
	}
	if err := Run(ctx, db, projectsDir); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	assertSchemaVersion(t, db, ctx, "20")
}

func TestMigrateToV20_WithoutProjectsTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v20-no-projects?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := migrateToV20(ctx, db); err != nil {
		t.Fatalf("migrateToV20() error = %v", err)
	}
	assertSchemaVersion(t, db, ctx, "20")
}
