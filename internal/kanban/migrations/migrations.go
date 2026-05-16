package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

const (
	currentSchemaVersion = 8
	schemaVersionKey     = "schema_version"
)

// Run applies incremental SQLite schema migrations up to the current version.
func Run(ctx context.Context, db *sql.DB) error {
	slog.Debug("checking schema version")
	version, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}
	slog.Debug("schema version detected", "version", version)
	migrations := []struct {
		version int
		run     func(context.Context, *sql.DB) error
	}{
		{2, migrateToV2},
		{3, migrateToV3},
		{4, migrateToV4},
		{5, migrateToV5},
		{6, migrateToV6},
		{7, migrateToV7},
		{8, migrateToV8},
	}
	for _, migration := range migrations {
		if err := applyMigration(ctx, db, version, migration.version, migration.run); err != nil {
			return err
		}
	}
	slog.Debug("schema migration complete", "version", currentSchemaVersion)
	return nil
}

func applyMigration(
	ctx context.Context,
	db *sql.DB,
	fromVersion int,
	toVersion int,
	run func(context.Context, *sql.DB) error,
) error {
	if fromVersion >= toVersion {
		return nil
	}
	if err := run(ctx, db); err != nil {
		return err
	}
	slog.Debug("migration applied", "version", toVersion)
	return nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, schemaVersionKey).Scan(&value)
	if err == nil {
		version, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil {
			return 0, fmt.Errorf("parse schema version %q: %w", value, parseErr)
		}
		return version, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return 1, nil
}

func setSchemaVersion(ctx context.Context, db *sql.DB, version int) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		schemaVersionKey, strconv.Itoa(version))
	if err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	return nil
}

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

func migrateToV5(ctx context.Context, db *sql.DB) error {
	hasCompletedAt, err := tableHasColumn(ctx, db, "tasks", "completed_at")
	if err != nil {
		return fmt.Errorf("check tasks.completed_at column: %w", err)
	}
	if !hasCompletedAt {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN completed_at TEXT`); err != nil {
			return fmt.Errorf("add tasks.completed_at column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 5)
}

func migrateToV6(ctx context.Context, db *sql.DB) error {
	indexesByTable := map[string][]string{
		"tasks": {
			`CREATE INDEX IF NOT EXISTS idx_tasks_project_state ON tasks(project_id, state)`,
		},
		"events": {
			`CREATE INDEX IF NOT EXISTS idx_events_task_created_at ON events(task_id, created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_events_project_created_at ON events(project_id, created_at)`,
		},
		"memories": {
			`CREATE INDEX IF NOT EXISTS idx_memories_scope_project ON memories(scope, project_id)`,
			`CREATE INDEX IF NOT EXISTS idx_memories_superseded_by ON memories(superseded_by)`,
		},
	}
	for table, indexes := range indexesByTable {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return fmt.Errorf("check %s table for schema migration v6: %w", table, err)
		}
		if !exists {
			continue
		}
		for _, ddl := range indexes {
			if _, err := db.ExecContext(ctx, ddl); err != nil {
				return fmt.Errorf("create schema migration v6 index: %w", err)
			}
		}
	}
	return setSchemaVersion(ctx, db, 6)
}

func migrateToV7(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "agent_profiles")
	if err != nil {
		return fmt.Errorf("check agent_profiles table for schema migration v7: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 7)
	}
	additions := []struct {
		name string
		ddl  string
	}{
		{"role", `ALTER TABLE agent_profiles ADD COLUMN role TEXT NOT NULL DEFAULT 'CODE_GEN'`},
		{"max_tokens", `ALTER TABLE agent_profiles ADD COLUMN max_tokens INTEGER NOT NULL DEFAULT 0`},
	}
	for _, col := range additions {
		has, err := tableHasColumn(ctx, db, "agent_profiles", col.name)
		if err != nil {
			return fmt.Errorf("check agent_profiles.%s column: %w", col.name, err)
		}
		if has {
			continue
		}
		if _, err := db.ExecContext(ctx, col.ddl); err != nil {
			return fmt.Errorf("add agent_profiles.%s column: %w", col.name, err)
		}
	}
	return setSchemaVersion(ctx, db, 7)
}

func migrateToV8(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil {
		return fmt.Errorf("check tasks table for schema migration v8: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 8)
	}
	has, err := tableHasColumn(ctx, db, "tasks", "success_criteria")
	if err != nil {
		return fmt.Errorf("check tasks.success_criteria column: %w", err)
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN success_criteria TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(success_criteria))`); err != nil {
			return fmt.Errorf("add tasks.success_criteria column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 8)
}
