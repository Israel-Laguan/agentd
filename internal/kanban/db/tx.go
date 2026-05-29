package db

import (
	"context"
	"database/sql"
	"fmt"
)

// SQLExecutor abstracts *sql.DB, *sql.Tx, and *ImmediateTx for write operations.
type SQLExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// SQLQueryer abstracts *sql.DB, *sql.Tx, and *ImmediateTx for read operations.
type SQLQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ImmediateTx is a SQLite "BEGIN IMMEDIATE" transaction that holds a single
// dedicated connection for the duration of the transaction. It implements
// SQLExecutor and SQLQueryer.
type ImmediateTx struct {
	conn *sql.Conn
	done bool
}

// BeginImmediate acquires a dedicated connection and opens a SQLite IMMEDIATE
// transaction that blocks concurrent writers until committed or rolled back.
func BeginImmediate(ctx context.Context, db *sql.DB) (*ImmediateTx, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("begin immediate transaction: %w", err)
	}
	return &ImmediateTx{conn: conn}, nil
}

func (tx *ImmediateTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.conn.ExecContext(ctx, query, args...)
}

func (tx *ImmediateTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.conn.QueryContext(ctx, query, args...)
}

func (tx *ImmediateTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.conn.QueryRowContext(ctx, query, args...)
}

// Commit commits the transaction and releases the connection.
func (tx *ImmediateTx) Commit() error {
	if tx.done {
		return nil
	}
	tx.done = true
	_, err := tx.conn.ExecContext(context.Background(), "COMMIT")
	if closeErr := tx.conn.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
}

// Rollback rolls back the transaction and releases the connection. Calling
// Rollback on an already-committed transaction is a no-op.
func (tx *ImmediateTx) Rollback() error {
	if tx.done {
		return nil
	}
	tx.done = true
	_, err := tx.conn.ExecContext(context.Background(), "ROLLBACK")
	if closeErr := tx.conn.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
}

// RollbackUnlessCommitted is a defer-friendly helper that rolls back tx if it
// has not been committed yet.
func RollbackUnlessCommitted(tx interface{ Rollback() error }) { _ = tx.Rollback() }
