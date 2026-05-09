package kanban

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agentd/internal/kanban/migrations"

	_ "modernc.org/sqlite"
)

//go:embed db/schema.sql
var schemaFS embed.FS

const schemaFile = "db/schema.sql"

// Open opens a SQLite database, applies operational pragmas, and runs schema
// migrations. The caller owns the returned database handle.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	if err := initialize(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func initialize(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	slog.Debug("applying sqlite pragmas")
	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
	}
	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("apply sqlite pragma %q: %w", pragma, err)
		}
	}
	if err := ensureTaskHeartbeatColumn(ctx, db); err != nil {
		return err
	}

	slog.Debug("applying schema")
	schema, err := schemaFS.ReadFile(schemaFile)
	if err != nil {
		return fmt.Errorf("read embedded schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if err := migrations.Run(ctx, db); err != nil {
		return err
	}

	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		return fmt.Errorf("read journal mode: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") && !strings.EqualFold(journalMode, "memory") {
		return fmt.Errorf("journal mode not WAL: %s", journalMode)
	}

	return nil
}

func ensureTaskHeartbeatColumn(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "tasks")
	if err != nil || !exists {
		return err
	}
	hasColumn, err := tableHasColumn(ctx, db, "tasks", "last_heartbeat")
	if err != nil || hasColumn {
		return err
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN last_heartbeat TEXT`); err != nil {
		return fmt.Errorf("add tasks.last_heartbeat column: %w", err)
	}
	return nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check table %s exists: %w", name, err)
	}
	return true, nil
}

func tableHasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, fmt.Errorf("read table info for %s: %w", table, err)
	}
	defer closeRows(rows)

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("scan table info for %s: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate table info for %s: %w", table, err)
	}
	return false, nil
}
