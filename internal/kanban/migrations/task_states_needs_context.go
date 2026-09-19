package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// migrateToV16 adds NEEDS_CONTEXT to the tasks.state CHECK constraint. The
// tiered pipeline declares the state in models.TaskState, but a CHECK that
// does not list it rejects the write at the DB layer, so the two have to move
// together. SQLite cannot alter a CHECK in place; the table is rebuilt.
func migrateToV16(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("check tasks table for v16: %w", err)
	}
	if !exists {
		// Partial schemas (and fresh databases created from schema.sql, which
		// already lists NEEDS_CONTEXT) have nothing to rebuild.
		return setSchemaVersion(ctx, db, 16)
	}
	createSQL, err := readTableSQL(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("read tasks schema for v16: %w", err)
	}
	if strings.Contains(createSQL, "'NEEDS_CONTEXT'") {
		return setSchemaVersion(ctx, db, 16)
	}

	// PRAGMA foreign_keys is connection-scoped in SQLite, so the OFF/ON pair
	// and the transaction must all run on the same dedicated connection.
	// Using the pooled *sql.DB for these calls risks the pool handing out a
	// different underlying connection for BeginTx, leaving FK enforcement ON
	// during the DROP TABLE below and cascading into dependent rows.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for schema migration v16: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for schema migration v16: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration v16: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := rebuildTasksTableV16(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		schemaVersionKey, strconv.Itoa(16)); err != nil {
		return fmt.Errorf("set schema version v16: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration v16: %w", err)
	}
	return nil
}

// rebuildTasksTableV16 copies tasks into a table carrying the widened CHECK
// constraint, swaps it in, and restores the indexes.
func rebuildTasksTableV16(ctx context.Context, tx *sql.Tx) error {
	steps := []struct {
		what string
		sql  string
	}{
		{"drop stale tasks_new", `DROP TABLE IF EXISTS tasks_new`},
		{"create tasks", createTasksV16SQL},
		{"copy tasks", copyTasksToV16SQL},
		{"drop old tasks table", `DROP TABLE tasks`},
		{"rename tasks", `ALTER TABLE tasks_new RENAME TO tasks`},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.sql); err != nil {
			return fmt.Errorf("%s v16: %w", step.what, err)
		}
	}
	for _, indexSQL := range tasksIndexSQL {
		if _, err := tx.ExecContext(ctx, indexSQL); err != nil {
			return fmt.Errorf("recreate tasks index v16: %w", err)
		}
	}
	return nil
}

// tasksIndexSQL recreates every index on tasks after a table rebuild. It must
// stay in sync with the indexes declared in internal/kanban/db/schema.sql.
var tasksIndexSQL = []string{
	`CREATE INDEX IF NOT EXISTS idx_tasks_state_assignee ON tasks(state, assignee)`,
	`CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id)`,
	`CREATE INDEX IF NOT EXISTS idx_tasks_project_state ON tasks(project_id, state)`,
	`CREATE INDEX IF NOT EXISTS idx_tasks_heartbeat ON tasks(last_heartbeat)`,
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
    os_process_id, started_at, completed_at, last_heartbeat, retry_count,
    token_usage, cached_token_usage, cache_write_token_usage,
    success_criteria, criteria_met, created_at, updated_at
)
SELECT
    id, project_id, agent_id, title, description, state, assignee,
    os_process_id, started_at, completed_at, last_heartbeat, retry_count,
    token_usage, cached_token_usage, cache_write_token_usage,
    success_criteria, criteria_met, created_at, updated_at
FROM tasks`
