package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateToV9AddsAgenticModeColumn(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v9?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v8SchemaSQL); err != nil {
		t.Fatalf("create v8 schema: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "13" {
		t.Fatalf("schema version = %q, want 13", version)
	}

	var hasColumn int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('agent_profiles') WHERE name = 'agentic_mode'`).Scan(&hasColumn); err != nil {
		t.Fatalf("check agent_profiles.agentic_mode column: %v", err)
	}
	if hasColumn != 1 {
		t.Fatalf("agentic_mode column present = %d, want 1", hasColumn)
	}

	var agenticMode int
	if err := db.QueryRowContext(ctx, `SELECT agentic_mode FROM agent_profiles WHERE id = 'default'`).Scan(&agenticMode); err != nil {
		t.Fatalf("read agentic_mode: %v", err)
	}
	if agenticMode != 0 {
		t.Fatalf("agentic_mode = %d, want 0", agenticMode)
	}
}

const v8SchemaSQL = `
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
    success_criteria TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(success_criteria) AND json_type(success_criteria) = 'array'),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE agent_profiles (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    temperature REAL NOT NULL DEFAULT 0.7,
    system_prompt TEXT,
    role TEXT NOT NULL DEFAULT 'CODE_GEN',
    max_tokens INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '8', datetime('now'));

INSERT INTO agent_profiles (id, name, provider, model, updated_at)
VALUES ('default', 'Default', 'openai', 'gpt-4', '2026-05-21T10:00:00Z');
`
