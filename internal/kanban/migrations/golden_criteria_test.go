package migrations

import (
	"context"
	"database/sql"
	"testing"
)

func TestV29PreservesExistingCriteria(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL);
	CREATE TABLE tasks (id TEXT PRIMARY KEY, success_criteria TEXT NOT NULL);
	INSERT INTO settings VALUES ('schema_version','28',datetime('now'));
	INSERT INTO tasks VALUES ('t','["existing"]');`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrateToV29(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var criteria, golden, version string
	if err := db.QueryRowContext(ctx, `SELECT success_criteria, golden_criteria FROM tasks WHERE id='t'`).Scan(&criteria, &golden); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if criteria != `["existing"]` || golden != `[]` || version != "29" {
		t.Fatalf("criteria=%s golden=%s version=%s", criteria, golden, version)
	}
}
