package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// migrateToV16 adds NEEDS_CONTEXT to the tasks.state CHECK constraint. The
// Go enum and transition table (models.TaskState) have accepted
// NEEDS_CONTEXT since the tiered escalation ladder was added, but the DB
// constraint was never updated to match, so persisting a task into that
// state fails at the DB layer. SQLite cannot ALTER a CHECK constraint in
// place, so this rebuilds the table exactly as v4/v2 did for earlier state
// additions.
func migrateToV16(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("check tasks table for v16: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 16)
	}
	createSQL, err := readTableSQL(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("read tasks schema for v16: %w", err)
	}
	if strings.Contains(createSQL, "'NEEDS_CONTEXT'") {
		return setSchemaVersion(ctx, db, 16)
	}
	if err := disableForeignKeys(ctx, db); err != nil {
		return err
	}
	defer func() { _, _ = db.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()
	return rebuildTasksTableV16(ctx, db)
}

func disableForeignKeys(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for schema migration v16: %w", err)
	}
	return nil
}

func rebuildTasksTableV16(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration v16: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, step := range []struct {
		sql string
		msg string
	}{
		{`DROP TABLE IF EXISTS tasks_new`, "drop stale tasks_new v16"},
		{createTasksV16SQL, "create tasks v16"},
		{copyTasksToV16SQL, "copy tasks v16"},
		{`DROP TABLE tasks`, "drop old tasks table v16"},
		{`ALTER TABLE tasks_new RENAME TO tasks`, "rename tasks v16"},
	} {
		if _, err := tx.ExecContext(ctx, step.sql); err != nil {
			return fmt.Errorf("%s: %w", step.msg, err)
		}
	}
	if err := recreateTasksIndexesV16(ctx, tx); err != nil {
		return err
	}
	return commitTasksRebuildV16(ctx, tx)
}

func recreateTasksIndexesV16(ctx context.Context, tx *sql.Tx) error {
	for _, ddl := range []string{
		`CREATE INDEX IF NOT EXISTS idx_tasks_state_assignee ON tasks(state, assignee)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_project_state ON tasks(project_id, state)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_heartbeat ON tasks(last_heartbeat)`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("recreate tasks index v16 (%s): %w", ddl, err)
		}
	}
	return nil
}

func commitTasksRebuildV16(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		schemaVersionKey, "16"); err != nil {
		return fmt.Errorf("set schema version v16: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration v16: %w", err)
	}
	return nil
}

const createTasksV16SQL = `
CREATE TABLE tasks_new (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'READY', 'QUEUED', 'RUNNING', 'BLOCKED', 'COMPLETED', 'FAILED', 'FAILED_REQUIRES_HUMAN', 'NEEDS_CONTEXT', 'IN_CONSIDERATION')),
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
    CHECK (retry_count >= 0),
    CHECK (token_usage >= 0),
    CHECK (cached_token_usage >= 0),
    CHECK (cache_write_token_usage >= 0),
    CHECK (json_valid(success_criteria)),
    CHECK (json_type(success_criteria) = 'array'),
    CHECK (json_valid(criteria_met)),
    CHECK (json_type(criteria_met) = 'array')
) STRICT`

const copyTasksToV16SQL = `
INSERT INTO tasks_new (
    id, project_id, agent_id, title, description, state, assignee,
    os_process_id, started_at, completed_at, last_heartbeat, retry_count, token_usage,
    cached_token_usage, cache_write_token_usage, success_criteria, criteria_met, created_at, updated_at
)
SELECT
    id, project_id, agent_id, title, description, state, assignee,
    os_process_id, started_at, completed_at, last_heartbeat,
    MAX(0, retry_count), MAX(0, token_usage),
    MAX(0, cached_token_usage), MAX(0, cache_write_token_usage),
    success_criteria, criteria_met, created_at, updated_at
FROM tasks`
