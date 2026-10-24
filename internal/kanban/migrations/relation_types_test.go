package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openMigrateV24DB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:migrate-v24?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, context.Background()
}

func seedV23Schema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
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
}

func assertPreservedRelations(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	type relation struct {
		parent string
		child  string
		kind   string
	}
	var preserved []relation
	rows, err := db.QueryContext(ctx, `
		SELECT parent_task_id, child_task_id, relation_type FROM task_relations`)
	if err != nil {
		t.Fatalf("query relations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r relation
		if err := rows.Scan(&r.parent, &r.child, &r.kind); err != nil {
			t.Fatalf("scan relation: %v", err)
		}
		preserved = append(preserved, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate relations: %v", err)
	}
	if len(preserved) != 1 {
		t.Fatalf("relations = %d, want 1 (existing edges must survive the rebuild)", len(preserved))
	}
	if got := preserved[0]; got != (relation{parent: "parent", child: "child", kind: "BLOCKS"}) {
		t.Fatalf("relation = %+v, want {parent:parent child:child kind:BLOCKS}", got)
	}
}

func TestMigrateToV24AcceptsSplitRelations(t *testing.T) {
	db, ctx := openMigrateV24DB(t)
	seedV23Schema(t, ctx, db)

	if err := migrateToV24(ctx, db); err != nil {
		t.Fatalf("migrateToV24: %v", err)
	}
	assertPreservedRelations(t, ctx, db)

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
