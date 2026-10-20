package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// aiCallsDDL mirrors the ai_calls block in kanban/db/schema.sql (a fresh
// database gets it from there first); both are idempotent.
var aiCallsDDL = []string{`
CREATE TABLE IF NOT EXISTS ai_calls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	at TEXT NOT NULL,
	kind TEXT NOT NULL,
	purpose TEXT NOT NULL,
	stage TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT '',
	task_id TEXT NOT NULL DEFAULT '',
	agent_id TEXT NOT NULL DEFAULT '',
	correlation_id TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL,
	model TEXT NOT NULL DEFAULT '',
	attempt TEXT NOT NULL,
	cascade INTEGER NOT NULL DEFAULT 0 CHECK (cascade IN (0, 1)),
	outcome TEXT NOT NULL,
	truncated INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
	message_count INTEGER NOT NULL DEFAULT 0 CHECK (message_count >= 0),
	tool_count INTEGER NOT NULL DEFAULT 0 CHECK (tool_count >= 0),
	input_chars INTEGER NOT NULL DEFAULT 0 CHECK (input_chars >= 0),
	output_chars INTEGER NOT NULL DEFAULT 0 CHECK (output_chars >= 0),
	tokens INTEGER NOT NULL DEFAULT 0 CHECK (tokens >= 0),
	cached_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cached_tokens >= 0),
	cache_write_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
	latency_ms INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms >= 0)
) STRICT`,
	`CREATE INDEX IF NOT EXISTS idx_ai_calls_at_purpose ON ai_calls(at, purpose, stage, model)`,
	`CREATE INDEX IF NOT EXISTS idx_ai_calls_task ON ai_calls(task_id) WHERE task_id != ''`,
	`CREATE INDEX IF NOT EXISTS idx_ai_calls_correlation ON ai_calls(correlation_id) WHERE correlation_id != ''`,
}

// migrateToV21 adds ai_calls: one row per provider attempt (T-044), sizes and
// counts only. No task foreign key: a record outlives its task.
func migrateToV21(ctx context.Context, db *sql.DB) error {
	for _, stmt := range aiCallsDDL {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create ai_calls: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 21)
}

// migrateToV22 adds the context mix and the truncation layer to ai_calls.
// A fresh database already has both from schema.sql, so each add is guarded.
func migrateToV22(ctx context.Context, db *sql.DB) error {
	for _, col := range []string{"context_mix", "truncated_by"} {
		has, err := tableHasColumn(ctx, db, "ai_calls", col)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := db.ExecContext(ctx, `ALTER TABLE ai_calls ADD COLUMN `+col+` TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add ai_calls.%s: %w", col, err)
		}
	}
	return setSchemaVersion(ctx, db, 22)
}
