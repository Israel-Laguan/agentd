package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateToV14AddsCriteriaMetColumn(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v14?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v13SchemaSQL); err != nil {
		t.Fatalf("create v13 schema: %v", err)
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

	var hasColumn int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name = 'criteria_met'`).Scan(&hasColumn); err != nil {
		t.Fatalf("check tasks.criteria_met column: %v", err)
	}
	if hasColumn != 1 {
		t.Fatalf("criteria_met column present = %d, want 1", hasColumn)
	}

	var criteriaMet string
	if err := db.QueryRowContext(ctx, `SELECT criteria_met FROM tasks WHERE id = 'task'`).Scan(&criteriaMet); err != nil {
		t.Fatalf("read criteria_met: %v", err)
	}
	if criteriaMet != "[]" {
		t.Fatalf("criteria_met = %q, want []", criteriaMet)
	}
}

// v13SchemaSQL is the tasks schema as it existed just before v14, including
// the success_criteria column added in v8 but without criteria_met.
const v13SchemaSQL = `
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
    success_criteria TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '13', datetime('now'));

INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
VALUES ('project', 'Project', 'input', 'workspace', 'ACTIVE', '2026-05-28T10:00:00Z', '2026-05-28T10:00:00Z');

INSERT INTO tasks (
    id, project_id, agent_id, title, description, state, assignee,
    os_process_id, started_at, last_heartbeat, retry_count, token_usage, success_criteria, created_at, updated_at
)
VALUES ('task', 'project', 'default', 'Task', 'description', 'READY', 'SYSTEM', NULL, NULL, NULL, 0, 0, '["file exists"]', '2026-05-28T10:00:00Z', '2026-05-28T10:00:00Z');
`
