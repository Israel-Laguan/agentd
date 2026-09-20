package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateToV16_AddsNeedsContextToCheckConstraint(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v16-needs-context?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v15SchemaWithoutNeedsContextSQL); err != nil {
		t.Fatalf("create v15 schema: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	assertSchemaVersion(t, db, ctx, "16")
	assertTasksTableContainsNeedsContext(t, db, ctx)
	assertMigratedTaskPreserved(t, db, ctx)
	assertClampedCountersZero(t, db, ctx)
	assertNeedsContextInsertAllowed(t, db, ctx)
	assertSchemaVersionAfterSecondRun(t, db, ctx, "16")
	assertNeedsContextStateAfterSecondRun(t, db, ctx)
}

func assertSchemaVersion(t *testing.T, db *sql.DB, ctx context.Context, want string) {
	t.Helper()
	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != want {
		t.Fatalf("schema version = %q, want %s", version, want)
	}
}

func assertTasksTableContainsNeedsContext(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	var createSQL string
	if err := db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'tasks'`).Scan(&createSQL); err != nil {
		t.Fatalf("read tasks ddl: %v", err)
	}
	if !strings.Contains(createSQL, "'NEEDS_CONTEXT'") {
		t.Fatalf("tasks ddl missing NEEDS_CONTEXT state: %s", createSQL)
	}
}

func assertMigratedTaskPreserved(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	var state, successCriteria string
	if err := db.QueryRowContext(ctx, `SELECT state, success_criteria FROM tasks WHERE id = 'task'`).Scan(&state, &successCriteria); err != nil {
		t.Fatalf("read migrated task: %v", err)
	}
	if state != "READY" {
		t.Fatalf("state = %q, want READY", state)
	}
	if successCriteria != "[]" {
		t.Fatalf("success_criteria = %q, want []", successCriteria)
	}
}

func assertClampedCountersZero(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	var retry, tokens, cached, writes int
	if err := db.QueryRowContext(ctx, `SELECT retry_count, token_usage, cached_token_usage, cache_write_token_usage FROM tasks WHERE id = 'negative-task'`).Scan(&retry, &tokens, &cached, &writes); err != nil {
		t.Fatalf("read clamped counters: %v", err)
	}
	if retry != 0 || tokens != 0 || cached != 0 || writes != 0 {
		t.Fatalf("clamped counters = %d,%d,%d,%d, want all zero", retry, tokens, cached, writes)
	}
}

func assertNeedsContextInsertAllowed(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (
		    id, project_id, agent_id, title, description, state, assignee,
		    retry_count, token_usage, cached_token_usage, cache_write_token_usage,
		    success_criteria, criteria_met, created_at, updated_at
		)
		VALUES ('needs-context-task', 'project', 'default', 'T', 'd', 'NEEDS_CONTEXT', 'SYSTEM',
		    0, 0, 0, 0, '[]', '[]', '2026-05-21T10:00:00Z', '2026-05-21T10:00:00Z')`); err != nil {
		t.Fatalf("insert NEEDS_CONTEXT task: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}
}

func assertSchemaVersionAfterSecondRun(t *testing.T, db *sql.DB, ctx context.Context, want string) {
	t.Helper()
	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version after second run: %v", err)
	}
	if version != want {
		t.Fatalf("schema version after second run = %q, want %s", version, want)
	}
}

func assertNeedsContextStateAfterSecondRun(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	var stateAfter string
	if err := db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = 'needs-context-task'`).Scan(&stateAfter); err != nil {
		t.Fatalf("read NEEDS_CONTEXT task after second run: %v", err)
	}
	if stateAfter != "NEEDS_CONTEXT" {
		t.Fatalf("state after second run = %q, want NEEDS_CONTEXT", stateAfter)
	}
}

const v15SchemaWithoutNeedsContextSQL = `
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
    updated_at TEXT NOT NULL,
    CHECK (json_valid(success_criteria)),
    CHECK (json_type(success_criteria) = 'array'),
    CHECK (json_valid(criteria_met)),
    CHECK (json_type(criteria_met) = 'array')
) STRICT;

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '15', datetime('now'));

INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
VALUES ('project', 'Project', 'input', 'workspace', 'ACTIVE', '2026-05-21T10:00:00Z', '2026-05-21T10:00:00Z');

INSERT INTO tasks (
    id, project_id, agent_id, title, description, state, assignee,
    retry_count, token_usage, cached_token_usage, cache_write_token_usage,
    success_criteria, criteria_met, created_at, updated_at
)
VALUES ('task', 'project', 'default', 'Task', 'description', 'READY', 'SYSTEM',
     0, 0, 0, 0, '[]', '[]', '2026-05-21T10:00:00Z', '2026-05-21T10:00:00Z');

INSERT INTO tasks (
    id, project_id, agent_id, title, description, state, assignee,
    retry_count, token_usage, cached_token_usage, cache_write_token_usage,
    success_criteria, criteria_met, created_at, updated_at
)
VALUES ('negative-task', 'project', 'default', 'Negative', 'description', 'READY', 'SYSTEM',
    -1, -2, -3, -4, '[]', '[]', '2026-05-21T10:00:00Z', '2026-05-21T10:00:00Z');
`
