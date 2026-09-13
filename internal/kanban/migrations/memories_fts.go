package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateToV3(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "memories")
	if err != nil {
		return fmt.Errorf("check memories table: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 3)
	}

	ftsExists, err := tableExists(ctx, db, "memories_fts")
	if err != nil {
		return fmt.Errorf("check memories_fts table: %w", err)
	}
	if ftsExists {
		return setSchemaVersion(ctx, db, 3)
	}

	columns := []struct {
		name string
		ddl  string
	}{
		{"last_accessed_at", "ALTER TABLE memories ADD COLUMN last_accessed_at TEXT"},
		{"access_count", "ALTER TABLE memories ADD COLUMN access_count INTEGER NOT NULL DEFAULT 0"},
		{"superseded_by", "ALTER TABLE memories ADD COLUMN superseded_by TEXT REFERENCES memories(id) ON DELETE SET NULL"},
	}
	for _, col := range columns {
		has, err := tableHasColumn(ctx, db, "memories", col.name)
		if err != nil {
			return fmt.Errorf("check column %s: %w", col.name, err)
		}
		if !has {
			if _, err := db.ExecContext(ctx, col.ddl); err != nil {
				return fmt.Errorf("add column %s: %w", col.name, err)
			}
		}
	}

	if _, err := db.ExecContext(ctx, memoriesFTSSQL); err != nil {
		return fmt.Errorf("create memories FTS table: %w", err)
	}
	if _, err := db.ExecContext(ctx, memoriesFTSPopulateSQL); err != nil {
		return fmt.Errorf("populate memories FTS: %w", err)
	}
	for _, triggerSQL := range memoriesFTSTriggers {
		if _, err := db.ExecContext(ctx, triggerSQL); err != nil {
			return fmt.Errorf("create FTS trigger: %w", err)
		}
	}

	return setSchemaVersion(ctx, db, 3)
}

const memoriesFTSSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    symptom,
    solution,
    tags,
    content='memories',
    content_rowid='rowid'
)`

const memoriesFTSPopulateSQL = `
INSERT OR IGNORE INTO memories_fts(rowid, symptom, solution, tags)
SELECT rowid, coalesce(symptom, ''), coalesce(solution, ''), coalesce(tags, '')
FROM memories`

var memoriesFTSTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
		INSERT INTO memories_fts(rowid, symptom, solution, tags)
		VALUES (new.rowid, coalesce(new.symptom, ''), coalesce(new.solution, ''), coalesce(new.tags, ''));
	END`,
	`CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, symptom, solution, tags)
		VALUES ('delete', old.rowid, coalesce(old.symptom, ''), coalesce(old.solution, ''), coalesce(old.tags, ''));
	END`,
	`CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, symptom, solution, tags)
		VALUES ('delete', old.rowid, coalesce(old.symptom, ''), coalesce(old.solution, ''), coalesce(old.tags, ''));
		INSERT INTO memories_fts(rowid, symptom, solution, tags)
		VALUES (new.rowid, coalesce(new.symptom, ''), coalesce(new.solution, ''), coalesce(new.tags, ''));
	END`,
}
