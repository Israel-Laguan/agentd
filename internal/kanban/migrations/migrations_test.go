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

func TestMigrateToV10AddsDisableTopicDriftColumn(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v10?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v9SchemaWithAgentProfilesSQL); err != nil {
		t.Fatalf("create v9 schema: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "12" {
		t.Fatalf("schema version = %q, want 12", version)
	}

	var hasColumn int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('agent_profiles') WHERE name = 'disable_topic_drift'`).Scan(&hasColumn); err != nil {
		t.Fatalf("check disable_topic_drift column: %v", err)
	}
	if hasColumn != 1 {
		t.Fatalf("disable_topic_drift column present = %d, want 1", hasColumn)
	}
}

func TestMigrateToV11AddsCapabilityRouteIntentColumn(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v11?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v10SchemaWithAgentProfilesSQL); err != nil {
		t.Fatalf("create v10 schema: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "12" {
		t.Fatalf("schema version = %q, want 12", version)
	}

	var hasColumn int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('agent_profiles') WHERE name = 'capability_route_intent'`).Scan(&hasColumn); err != nil {
		t.Fatalf("check capability_route_intent column: %v", err)
	}
	if hasColumn != 1 {
		t.Fatalf("capability_route_intent column present = %d, want 1", hasColumn)
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
	if version != "12" {
		t.Fatalf("schema version = %q, want 12", version)
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

const v9SchemaWithAgentProfilesSQL = `
CREATE TABLE projects (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    original_input TEXT NOT NULL,
    workspace_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
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
    agentic_mode INTEGER NOT NULL DEFAULT 0 CHECK (agentic_mode IN (0, 1)),
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at)
VALUES ('schema_version', '9', datetime('now'));`

const v10SchemaWithAgentProfilesSQL = `
CREATE TABLE projects (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    original_input TEXT NOT NULL,
    workspace_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
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
    agentic_mode INTEGER NOT NULL DEFAULT 0 CHECK (agentic_mode IN (0, 1)),
    disable_topic_drift INTEGER NOT NULL DEFAULT 0 CHECK (disable_topic_drift IN (0, 1)),
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at)
VALUES ('schema_version', '10', datetime('now'));`

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
