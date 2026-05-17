package migrations

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestMigrateToV2PreservesTasksAndAllowsBlockedState(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v2?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, oldSchemaSQL); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES ('project', 'Project', 'input', 'workspace', 'ACTIVE', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (
			id, project_id, agent_id, title, description, state, assignee,
			os_process_id, started_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
		)
		VALUES ('task', 'project', 'default', 'Task', 'description', 'READY', 'SYSTEM', NULL, NULL, NULL, 0, 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = 'task' AND state = 'READY'`).Scan(&count); err != nil {
		t.Fatalf("count migrated tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("migrated task count = %d, want 1", count)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (
			id, project_id, agent_id, title, description, state, assignee,
			os_process_id, started_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
		)
		VALUES ('blocked', 'project', 'default', 'Blocked', 'description', 'BLOCKED', 'SYSTEM', NULL, NULL, NULL, 0, 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert BLOCKED task after migration: %v", err)
	}
}

func TestMigrateToV4AllowsFailedRequiresHumanState(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v4-v5?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v3SchemaSQL); err != nil {
		t.Fatalf("create v3 schema: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES ('project', 'Project', 'input', 'workspace', 'ACTIVE', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (
			id, project_id, agent_id, title, description, state, assignee,
			os_process_id, started_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
		)
		VALUES ('task', 'project', 'default', 'Task', 'description', 'FAILED', 'SYSTEM', NULL, NULL, NULL, 3, 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = 'task' AND state = 'FAILED'`).Scan(&count); err != nil {
		t.Fatalf("count migrated tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("migrated task count = %d, want 1", count)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE tasks SET state = 'FAILED_REQUIRES_HUMAN' WHERE id = 'task'`); err != nil {
		t.Fatalf("set FAILED_REQUIRES_HUMAN after migration: %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "8" {
		t.Fatalf("schema version = %q, want 8", version)
	}

	var completedAt sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT completed_at FROM tasks WHERE id = 'task'`).Scan(&completedAt); err != nil {
		t.Fatalf("read completed_at column: %v", err)
	}
	var successCriteria string
	if err := db.QueryRowContext(ctx, `SELECT success_criteria FROM tasks WHERE id = 'task'`).Scan(&successCriteria); err != nil {
		t.Fatalf("read success_criteria column: %v", err)
	}
	if successCriteria != "[]" {
		t.Fatalf("success_criteria = %q, want []", successCriteria)
	}
}

const v3SchemaSQL = `
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

INSERT INTO settings (key, value, updated_at)
VALUES ('schema_version', '3', datetime('now'));`

const oldSchemaSQL = `
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
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'READY', 'QUEUED', 'RUNNING', 'COMPLETED', 'FAILED', 'IN_CONSIDERATION')),
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
) STRICT;`
