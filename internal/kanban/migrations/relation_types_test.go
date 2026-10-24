package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateToV24AcceptsSplitRelations(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v24?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	for _, stmt := range []string{
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY NOT NULL) STRICT`,
		`INSERT INTO tasks (id) VALUES ('parent'), ('child'), ('split-child')`,
		`CREATE TABLE task_relations (
			parent_task_id TEXT NOT NULL,
			child_task_id TEXT NOT NULL,
			relation_type TEXT NOT NULL DEFAULT 'BLOCKS',
			PRIMARY KEY (parent_task_id, child_task_id),
			CHECK (parent_task_id != child_task_id),
			CHECK (relation_type IN ('BLOCKS', 'SPAWNED_BY', 'DEPENDS_ON'))
		) STRICT`,
		`INSERT INTO task_relations (parent_task_id, child_task_id, relation_type)
			VALUES ('parent', 'child', 'BLOCKS')`,
		`INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '23', datetime('now'))`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed v23 schema: %v", err)
		}
	}

	if err := migrateToV24(ctx, db); err != nil {
		t.Fatalf("migrateToV24: %v", err)
	}

	var relations int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_relations`).Scan(&relations); err != nil {
		t.Fatalf("count relations: %v", err)
	}
	if relations != 1 {
		t.Fatalf("relations = %d, want 1 (existing edges must survive the rebuild)", relations)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO task_relations (parent_task_id, child_task_id, relation_type)
		VALUES ('parent', 'split-child', 'SPLIT')`); err != nil {
		t.Fatalf("insert SPLIT relation: %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "24" {
		t.Fatalf("schema version = %q, want 24", version)
	}

	if err := migrateToV24(ctx, db); err != nil {
		t.Fatalf("migrateToV24 second run: %v", err)
	}
}
