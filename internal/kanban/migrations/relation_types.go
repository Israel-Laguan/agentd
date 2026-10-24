package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// migrateToV24 adds SPLIT to the task_relations.relation_type CHECK
// constraint. The split stage marks the ancestry it creates with SPLIT so the
// depth cap follows split generations only, not ordinary BLOCKS dependencies.
// SQLite cannot ALTER a CHECK constraint in place, so this rebuilds the table
// exactly as earlier state additions did.
func migrateToV24(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "task_relations")
	if err != nil {
		return fmt.Errorf("check task_relations table for v24: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 24)
	}
	createSQL, err := readTableSQL(ctx, db, "task_relations")
	if err != nil {
		return fmt.Errorf("read task_relations schema for v24: %w", err)
	}
	if strings.Contains(createSQL, "'SPLIT'") {
		return setSchemaVersion(ctx, db, 24)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for schema migration v24: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for schema migration v24: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`) }()
	return rebuildTaskRelationsTableV24(ctx, conn)
}

func rebuildTaskRelationsTableV24(ctx context.Context, db interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration v24: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, step := range []struct {
		sql string
		msg string
	}{
		{`DROP TABLE IF EXISTS task_relations_new`, "drop stale task_relations_new v24"},
		{createTaskRelationsV24SQL, "create task_relations v24"},
		{copyTaskRelationsV24SQL, "copy task_relations v24"},
		{`DROP TABLE task_relations`, "drop old task_relations table v24"},
		{`ALTER TABLE task_relations_new RENAME TO task_relations`, "rename task_relations v24"},
	} {
		if _, err := tx.ExecContext(ctx, step.sql); err != nil {
			return fmt.Errorf("%s: %w", step.msg, err)
		}
	}
	for _, ddl := range []string{
		`CREATE INDEX IF NOT EXISTS idx_task_relations_child ON task_relations(child_task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_relations_parent ON task_relations(parent_task_id)`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("recreate task_relations index v24 (%s): %w", ddl, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		schemaVersionKey, "24"); err != nil {
		return fmt.Errorf("set schema version v24: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration v24: %w", err)
	}
	return nil
}

const createTaskRelationsV24SQL = `
CREATE TABLE task_relations_new (
    parent_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    child_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL DEFAULT 'BLOCKS',
    PRIMARY KEY (parent_task_id, child_task_id),
    CHECK (parent_task_id != child_task_id),
    CHECK (relation_type IN ('BLOCKS', 'SPAWNED_BY', 'DEPENDS_ON', 'SPLIT'))
) STRICT`

const copyTaskRelationsV24SQL = `
INSERT INTO task_relations_new (parent_task_id, child_task_id, relation_type)
SELECT parent_task_id, child_task_id, relation_type FROM task_relations`
