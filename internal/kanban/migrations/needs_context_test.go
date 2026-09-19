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

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "16" {
		t.Fatalf("schema version = %q, want 16", version)
	}

	var createSQL string
	if err := db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'tasks'`).Scan(&createSQL); err != nil {
		t.Fatalf("read tasks ddl: %v", err)
	}
	if !strings.Contains(createSQL, "'NEEDS_CONTEXT'") {
		t.Fatalf("tasks ddl missing NEEDS_CONTEXT state: %s", createSQL)
	}

	// The pre-existing row must survive the table rebuild intact.
	var state string
	var successCriteria string
	if err := db.QueryRowContext(ctx, `SELECT state, success_criteria FROM tasks WHERE id = 'task'`).Scan(&state, &successCriteria); err != nil {
		t.Fatalf("read migrated task: %v", err)
	}
	if state != "READY" {
		t.Fatalf("state = %q, want READY", state)
	}
	if successCriteria != "[]" {
		t.Fatalf("success_criteria = %q, want []", successCriteria)
	}

	// The constraint must now accept NEEDS_CONTEXT.
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
`
