package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"agentd/internal/kanban/migrations"
)

//go:embed schema.sql
var schemaFS embed.FS

const schemaFile = "schema.sql"

// Open opens a SQLite database, applies operational pragmas, and runs schema
// migrations. The caller owns the returned database handle.
func Open(path string, projectsDir string) (*sql.DB, error) {
	unlock, err := lockSetup(path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	db, err := sql.Open("sqlite", connectionDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	if err := initialize(db, projectsDir); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

// lockSetup serializes Open's schema setup between handles and processes on one
// database: two that both see a column missing both add it, and the second
// fails. It holds a write lock on the side database path+".lock". The schema
// setup itself cannot take the lock, because migrations run their own
// transactions and PRAGMA foreign_keys, which SQLite ignores inside one. The OS
// drops the lock when the process dies. The holder's setup is capped at 10s, so
// the 30s wait covers it.
// shortcut: file: URIs are not locked (tests' in-memory databases); lock them
// too if production ever opens one.
func lockSetup(path string) (func(), error) {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return func() {}, nil
	}
	lock, err := sql.Open("sqlite", path+".lock?_pragma="+url.QueryEscape("busy_timeout(30000)"))
	if err != nil {
		return nil, fmt.Errorf("open schema setup lock: %w", err)
	}
	ctx := context.Background()
	conn, err := lock.Conn(ctx)
	if err == nil {
		if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			_ = conn.Close()
		}
	}
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("take schema setup lock: %w", err)
	}
	return func() {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		_ = conn.Close()
		_ = lock.Close()
	}, nil
}

// connectionPragmas are applied by the driver to every pooled connection.
// busy_timeout and foreign_keys are per-connection settings, so running them
// once through the pool leaves every other connection with a 0ms busy timeout
// and writes failing immediately with SQLITE_BUSY under contention.
var connectionPragmas = []string{
	"foreign_keys(1)",
	"busy_timeout(5000)",
	"synchronous(NORMAL)",
}

// connectionDSN appends the per-connection pragmas to path, which may be a
// plain file path or a file: URI that already carries query parameters.
func connectionDSN(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var b strings.Builder
	b.WriteString(path)
	for _, p := range connectionPragmas {
		b.WriteString(sep)
		b.WriteString("_pragma=")
		b.WriteString(url.QueryEscape(p))
		sep = "&"
	}
	return b.String()
}

func initialize(db *sql.DB, projectsDir string) error {
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
	if err := migrations.Run(ctx, db, projectsDir); err != nil {
		return err
	}
	if err := NormalizeStoredTimestamps(ctx, db); err != nil {
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
	defer func() { _ = rows.Close() }()

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
