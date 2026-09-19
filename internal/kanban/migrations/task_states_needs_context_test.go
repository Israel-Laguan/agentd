package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigrationV16AddsNeedsContextState proves an existing database whose
// tasks.state CHECK predates NEEDS_CONTEXT is widened to accept it, and that
// the rows and indexes survive the table rebuild.
func TestMigrationV16AddsNeedsContextState(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v16?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v15TasksSchemaSQL); err != nil {
		t.Fatalf("create pre-v16 schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES ('p1', 'proj', 'input', '/tmp/ws-v16', 'ACTIVE', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (id, project_id, agent_id, title, description, state, assignee, created_at, updated_at)
		VALUES ('t1', 'p1', 'default', 'existing', 'keep me', 'COMPLETED', 'SYSTEM', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	// Before the migration the state must be rejected.
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET state = 'NEEDS_CONTEXT' WHERE id = 't1'`); err == nil {
		t.Fatal("pre-v16 schema accepted NEEDS_CONTEXT; fixture is not representative")
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "16" {
		t.Fatalf("schema version = %q, want 16", version)
	}

	if _, err := db.ExecContext(ctx, `UPDATE tasks SET state = 'NEEDS_CONTEXT' WHERE id = 't1'`); err != nil {
		t.Fatalf("post-v16 schema still rejects NEEDS_CONTEXT: %v", err)
	}

	assertV16RebuildPreservedData(ctx, t, db)
}

// assertV16RebuildPreservedData checks the table rebuild kept rows, indexes,
// and the narrowing half of the CHECK constraint.
func assertV16RebuildPreservedData(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	var description string
	if err := db.QueryRowContext(ctx, `SELECT description FROM tasks WHERE id = 't1'`).Scan(&description); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if description != "keep me" {
		t.Errorf("row lost in rebuild: description = %q, want %q", description, "keep me")
	}

	var indexes int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'tasks'
		  AND name IN ('idx_tasks_state_assignee','idx_tasks_project','idx_tasks_project_state','idx_tasks_heartbeat')`).
		Scan(&indexes); err != nil {
		t.Fatalf("count tasks indexes: %v", err)
	}
	if indexes != 4 {
		t.Errorf("tasks indexes after rebuild = %d, want 4", indexes)
	}

	// An unknown state must still be rejected: the CHECK was widened, not dropped.
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET state = 'NOPE' WHERE id = 't1'`); err == nil {
		t.Error("post-v16 schema accepts an unknown state; the CHECK was dropped, not widened")
	}
}

// TestMigrationV16IsIdempotent guards the re-run path.
func TestMigrationV16IsIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v16-idem?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v15TasksSchemaSQL); err != nil {
		t.Fatalf("create pre-v16 schema: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := Run(ctx, db); err != nil {
			t.Fatalf("Run() pass %d error = %v", i+1, err)
		}
	}
	createSQL, err := readTableSQL(ctx, db, "tasks")
	if err != nil {
		t.Fatalf("read tasks schema: %v", err)
	}
	if strings.Count(createSQL, "'NEEDS_CONTEXT'") != 1 {
		t.Errorf("NEEDS_CONTEXT appears %d times in the CHECK, want 1",
			strings.Count(createSQL, "'NEEDS_CONTEXT'"))
	}
}

// v15TasksSchemaSQL is the tasks table as it stood before v16, plus the
// settings and projects tables the migration runner needs.
const v15TasksSchemaSQL = `
CREATE TABLE projects (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    original_input TEXT NOT NULL,
    workspace_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE tasks (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'READY', 'QUEUED', 'RUNNING', 'BLOCKED', 'COMPLETED', 'FAILED', 'FAILED_REQUIRES_HUMAN', 'IN_CONSIDERATION')),
    assignee TEXT NOT NULL DEFAULT 'SYSTEM' CHECK (assignee IN ('SYSTEM', 'HUMAN')),
    os_process_id INTEGER,
    started_at TEXT,
    completed_at TEXT,
    last_heartbeat TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    token_usage INTEGER NOT NULL DEFAULT 0,
    cached_token_usage INTEGER NOT NULL DEFAULT 0,
    cache_write_token_usage INTEGER NOT NULL DEFAULT 0,
    success_criteria TEXT NOT NULL DEFAULT '[]',
    criteria_met TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE INDEX idx_tasks_state_assignee ON tasks(state, assignee);
CREATE INDEX idx_tasks_project ON tasks(project_id);
CREATE INDEX idx_tasks_project_state ON tasks(project_id, state);
CREATE INDEX idx_tasks_heartbeat ON tasks(last_heartbeat);

CREATE TABLE settings (
    key TEXT PRIMARY KEY NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '15', '2026-01-01T00:00:00Z');
`
