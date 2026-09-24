package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	currentSchemaVersion = 18
	schemaVersionKey     = "schema_version"
)

// Run applies incremental SQLite schema migrations up to the current version.
func Run(ctx context.Context, db *sql.DB, projectsDir string) error {
	if strings.TrimSpace(projectsDir) == "" {
		return fmt.Errorf("projects_dir must not be empty")
	}
	projectsDir = filepath.Clean(projectsDir)
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
		{9, migrateToV9},
		{10, migrateToV10},
		{11, migrateToV11},
		{12, migrateToV12},
		{13, migrateToV13},
		{14, migrateToV14},
		{15, migrateToV15},
		{16, migrateToV16},
		{17, migrateToV17},
		{18, func(ctx context.Context, db *sql.DB) error {
			return migrateWorkspacePaths(ctx, db, projectsDir, v18MigrateMode)
		}},
	}
	for _, migration := range migrations {
		if err := applyMigration(ctx, db, version, migration.version, migration.run); err != nil {
			return err
		}
	}
	if version >= currentSchemaVersion {
		return migrateWorkspacePaths(ctx, db, projectsDir, v18ValidateMode)
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
