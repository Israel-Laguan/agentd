package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateToV2SkipsWhenBlockedAlreadyInSchema(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v2-skip?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v1SchemaWithBlockedSQL); err != nil {
		t.Fatalf("create v1 schema with BLOCKED: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "14" {
		t.Fatalf("schema version = %q, want 14", version)
	}

	var createSQL string
	if err := db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'tasks'`).Scan(&createSQL); err != nil {
		t.Fatalf("read tasks ddl: %v", err)
	}
	if !strings.Contains(createSQL, "'BLOCKED'") {
		t.Fatalf("tasks ddl missing BLOCKED state: %s", createSQL)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = 'task' AND state = 'READY'`).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("task count = %d, want 1", count)
	}
}

const v1SchemaWithBlockedSQL = `
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
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'READY', 'QUEUED', 'RUNNING', 'BLOCKED', 'COMPLETED', 'FAILED', 'IN_CONSIDERATION')),
    assignee TEXT NOT NULL DEFAULT 'SYSTEM' CHECK (assignee IN ('SYSTEM', 'HUMAN')),
    os_process_id INTEGER,
    started_at TEXT,
    last_heartbeat TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    token_usage INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '1', datetime('now'));

INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
VALUES ('project', 'Project', 'input', 'workspace', 'ACTIVE', '2026-05-21T10:00:00Z', '2026-05-21T10:00:00Z');

INSERT INTO tasks (
    id, project_id, agent_id, title, description, state, assignee,
    os_process_id, started_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
)
VALUES ('task', 'project', 'default', 'Task', 'description', 'READY', 'SYSTEM', NULL, NULL, NULL, 0, 0, '2026-05-21T10:00:00Z', '2026-05-21T10:00:00Z');
`
