package migrations

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigrateToV19_AddsProjectUserID covers the J11 migration: projects carry
// the requesting identity so the worker can scope memory recall (and hence
// preference recall) to that user.
func TestMigrateToV19_AddsProjectUserID(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v19-user-id?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	projectsDir := t.TempDir()
	// The seeded project's workspace path must live under projectsDir: the
	// v18 workspace-path validator (which runs on every migration pass) rejects
	// any project pointing outside the root, independently of this migration.
	workspace := projectsDir + "/p1"
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
		workspace); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	if err := Run(ctx, db, projectsDir); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var userID string
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM projects WHERE id = 'p1'`).Scan(&userID); err != nil {
		t.Fatalf("read projects.user_id: %v", err)
	}
	if userID != "" {
		t.Fatalf("pre-existing project user_id = %q, want \"\" (migrated rows have no known user)", userID)
	}

	// The column must be writable and round-trip a real value.
	if _, err := db.ExecContext(ctx, `UPDATE projects SET user_id = 'alice' WHERE id = 'p1'`); err != nil {
		t.Fatalf("update projects.user_id: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM projects WHERE id = 'p1'`).Scan(&userID); err != nil {
		t.Fatalf("re-read projects.user_id: %v", err)
	}
	if userID != "alice" {
		t.Fatalf("projects.user_id = %q, want %q", userID, "alice")
	}

	// Running again must be a no-op, not a duplicate-column error.
	if err := Run(ctx, db, projectsDir); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
}

// TestMigrateToV19_WithoutProjectsTable covers the guard for a database that
// has not yet bootstrapped its schema: the migration records the version and
// does not attempt the ALTER.
func TestMigrateToV19_WithoutProjectsTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v19-no-projects?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := setSchemaVersion(ctx, db, 18); err != nil {
		t.Fatalf("seed schema version: %v", err)
	}

	if err := migrateToV19(ctx, db); err != nil {
		t.Fatalf("migrateToV19() error = %v", err)
	}
	assertSchemaVersion(t, db, ctx, "19")
}

// v18SchemaWithoutUserIDSQL is the projects table as it existed before v19.
const v18SchemaWithoutUserIDSQL = `
CREATE TABLE projects (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    original_input TEXT NOT NULL,
    workspace_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;
`
