package kanban

import (
	"context"
	"database/sql"
	"fmt"
)

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type sqlQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type rowsAffected interface {
	RowsAffected() (int64, error)
}

type immediateTx struct {
	conn *sql.Conn
	done bool
}

func beginImmediate(ctx context.Context, db *sql.DB) (*immediateTx, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("begin immediate transaction: %w", err)
	}
	return &immediateTx{conn: conn}, nil
}

func (tx *immediateTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.conn.ExecContext(ctx, query, args...)
}

func (tx *immediateTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.conn.QueryContext(ctx, query, args...)
}

func (tx *immediateTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.conn.QueryRowContext(ctx, query, args...)
}

func (tx *immediateTx) Commit() error {
	if tx.done {
		return nil
	}
	if _, err := tx.conn.ExecContext(context.Background(), "COMMIT"); err != nil {
		return err
	}
	tx.done = true
	return tx.conn.Close()
}

func (tx *immediateTx) Rollback() error {
	if tx.done {
		return nil
	}
	if _, err := tx.conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		_ = tx.conn.Close()
		return err
	}
	tx.done = true
	return tx.conn.Close()
}

func rollbackUnlessCommitted(tx interface{ Rollback() error }) { _ = tx.Rollback() }
