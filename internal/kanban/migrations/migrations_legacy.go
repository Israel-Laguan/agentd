package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

func migrateToV2(ctx context.Context, db *sql.DB) error {
	createSQL, err := readTableSQL(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("read tasks schema for v2: %w", err)
	}
	if strings.Contains(createSQL, "'BLOCKED'") {
		return setSchemaVersion(ctx, db, 2)
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for schema migration v2: %w", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration v2: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tasks_new`); err != nil {
		return fmt.Errorf("drop stale tasks_new: %w", err)
	}
	if _, err := tx.ExecContext(ctx, createTasksV2SQL); err != nil {
		return fmt.Errorf("create tasks v2: %w", err)
	}
	if _, err := tx.ExecContext(ctx, copyTasksToV2SQL); err != nil {
		return fmt.Errorf("copy tasks v2: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE tasks`); err != nil {
		return fmt.Errorf("drop old tasks table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tasks_new RENAME TO tasks`); err != nil {
		return fmt.Errorf("rename tasks v2: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_state_assignee ON tasks(state, assignee)`); err != nil {
		return fmt.Errorf("recreate tasks state index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id)`); err != nil {
		return fmt.Errorf("recreate tasks project index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_heartbeat ON tasks(last_heartbeat)`); err != nil {
		return fmt.Errorf("recreate tasks heartbeat index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		schemaVersionKey, strconv.Itoa(currentSchemaVersion)); err != nil {
		return fmt.Errorf("set schema version v2: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration v2: %w", err)
	}
	return nil
}

const createTasksV2SQL = `
CREATE TABLE tasks_new (
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
) STRICT`

const copyTasksToV2SQL = `
INSERT INTO tasks_new (
    id, project_id, agent_id, title, description, state, assignee,
    os_process_id, started_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
)
SELECT
    id, project_id, agent_id, title, description, state, assignee,
    os_process_id, started_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
FROM tasks`

func migrateToV4(ctx context.Context, db *sql.DB) error {
	createSQL, err := readTableSQL(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("read tasks schema for v4: %w", err)
	}
	if strings.Contains(createSQL, "'FAILED_REQUIRES_HUMAN'") {
		return setSchemaVersion(ctx, db, 4)
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for schema migration v4: %w", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration v4: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tasks_new`); err != nil {
		return fmt.Errorf("drop stale tasks_new v4: %w", err)
	}
	if _, err := tx.ExecContext(ctx, createTasksV4SQL); err != nil {
		return fmt.Errorf("create tasks v4: %w", err)
	}
	if _, err := tx.ExecContext(ctx, copyTasksToV2SQL); err != nil {
		return fmt.Errorf("copy tasks v4: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE tasks`); err != nil {
		return fmt.Errorf("drop old tasks table v4: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tasks_new RENAME TO tasks`); err != nil {
		return fmt.Errorf("rename tasks v4: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_state_assignee ON tasks(state, assignee)`); err != nil {
		return fmt.Errorf("recreate tasks state index v4: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id)`); err != nil {
		return fmt.Errorf("recreate tasks project index v4: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_heartbeat ON tasks(last_heartbeat)`); err != nil {
		return fmt.Errorf("recreate tasks heartbeat index v4: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		schemaVersionKey, strconv.Itoa(4)); err != nil {
		return fmt.Errorf("set schema version v4: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration v4: %w", err)
	}
	return nil
}

func readTableSQL(ctx context.Context, db *sql.DB, tableName string) (string, error) {
	var createSQL string
	err := db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&createSQL)
	if err != nil {
		return "", err
	}
	return createSQL, nil
}

const createTasksV4SQL = `
CREATE TABLE tasks_new (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'READY', 'QUEUED', 'RUNNING', 'BLOCKED', 'COMPLETED', 'FAILED', 'FAILED_REQUIRES_HUMAN', 'IN_CONSIDERATION')),
    assignee TEXT NOT NULL DEFAULT 'SYSTEM' CHECK (assignee IN ('SYSTEM', 'HUMAN')),
    os_process_id INTEGER,
    started_at TEXT,
    last_heartbeat TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    token_usage INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT`
