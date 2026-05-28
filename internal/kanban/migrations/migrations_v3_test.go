package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateToV3AddsMemoriesFTSColumnsAndTriggers(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v3?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v2SchemaWithMemoriesSQL); err != nil {
		t.Fatalf("create v2 schema with memories: %v", err)
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

	for _, col := range []string{"last_accessed_at", "access_count", "superseded_by"} {
		var hasColumn int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pragma_table_info('memories') WHERE name = ?`, col).Scan(&hasColumn); err != nil {
			t.Fatalf("check memories.%s column: %v", col, err)
		}
		if hasColumn != 1 {
			t.Fatalf("memories.%s column present = %d, want 1", col, hasColumn)
		}
	}

	var ftsExists int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'memories_fts'`).Scan(&ftsExists); err != nil {
		t.Fatalf("check memories_fts table: %v", err)
	}
	if ftsExists != 1 {
		t.Fatalf("memories_fts table exists = %d, want 1", ftsExists)
	}

	var triggerCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'trigger' AND name IN ('memories_ai', 'memories_ad', 'memories_au')`).Scan(&triggerCount); err != nil {
		t.Fatalf("check memories FTS triggers: %v", err)
	}
	if triggerCount != 3 {
		t.Fatalf("memories FTS trigger count = %d, want 3", triggerCount)
	}
}

func TestMigrateToV3SkipsWhenFTSAlreadyExists(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v3-fts-skip?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v2SchemaWithMemoriesSQL); err != nil {
		t.Fatalf("create v2 schema with memories: %v", err)
	}
	for _, ddl := range []string{
		`ALTER TABLE memories ADD COLUMN last_accessed_at TEXT`,
		`ALTER TABLE memories ADD COLUMN access_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE memories ADD COLUMN superseded_by TEXT`,
		memoriesFTSSQL,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("precreate v3 artifacts: %v", err)
		}
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
}

func TestMigrateToV3SkipsWithoutMemoriesTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v3-skip?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v2SchemaSQL); err != nil {
		t.Fatalf("create v2 schema: %v", err)
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

	var tableExists int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'memories'`).Scan(&tableExists); err != nil {
		t.Fatalf("check memories table: %v", err)
	}
	if tableExists != 0 {
		t.Fatalf("memories table exists = %d, want 0", tableExists)
	}
}

const v2SchemaSQL = `
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

INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '2', datetime('now'));
`

const v2SchemaWithMemoriesSQL = v2SchemaSQL + `
CREATE TABLE memories (
    id TEXT PRIMARY KEY NOT NULL,
    scope TEXT NOT NULL DEFAULT 'GLOBAL',
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    tags TEXT,
    symptom TEXT,
    solution TEXT,
    created_at TEXT NOT NULL
) STRICT;

INSERT INTO memories (id, scope, symptom, solution, tags, created_at)
VALUES ('mem-1', 'GLOBAL', 'symptom', 'solution', 'tag', '2026-05-21T10:00:00Z');
`
