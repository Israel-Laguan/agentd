package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationAddsRoleAndMaxTokensColumns(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v7?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v6SchemaSQL); err != nil {
		t.Fatalf("create v6 schema: %v", err)
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

	for _, col := range []string{"role", "max_tokens"} {
		var hasColumn int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pragma_table_info('agent_profiles') WHERE name = ?`, col).Scan(&hasColumn); err != nil {
			t.Fatalf("check agent_profiles.%s column: %v", col, err)
		}
		if hasColumn != 1 {
			t.Fatalf("agent_profiles.%s column present = %d, want 1", col, hasColumn)
		}
	}

	var role string
	if err := db.QueryRowContext(ctx, `SELECT role FROM agent_profiles WHERE id = 'default'`).Scan(&role); err != nil {
		t.Fatalf("read agent_profiles.role: %v", err)
	}
	if role != "CODE_GEN" {
		t.Fatalf("role = %q, want CODE_GEN", role)
	}
}

const v6SchemaSQL = `
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
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '6', datetime('now'));

INSERT INTO agent_profiles (id, name, provider, model, updated_at)
VALUES ('default', 'Default', 'openai', 'gpt-4', '2026-05-21T10:00:00Z');
`

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

func TestMigrationAddsDisableTopicDriftColumn(t *testing.T) {
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
	if version != "16" {
		t.Fatalf("schema version = %q, want 16", version)
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

func TestMigrationAddsCapabilityRouteIntentColumn(t *testing.T) {
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
	if version != "16" {
		t.Fatalf("schema version = %q, want 16", version)
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
