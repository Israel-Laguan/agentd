package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateToV19 adds projects.user_id, the identity that requested the project.
//
// Task execution-time memory recall is scoped by user (see
// kanban.RecallMemories, which only adds the USER_PREFERENCE branch when
// UserID is non-empty), but the worker had no way to learn which user to
// recall for: nothing carried the requester's identity from the chat turn
// through materialization to execution. Stamping it on the project rather than
// on each task keeps it to one column and makes it correct for tasks created
// later (phase continuation, breakdowns, handoffs), which would each have had
// to be stamped separately on a per-task column.
//
// Empty for pre-existing projects, which simply means no preferences are
// recalled for them — the recall query treats a blank user_id as "no
// preferences", not as "all preferences".
func migrateToV19(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "projects")
	if err != nil {
		return fmt.Errorf("check projects table: %w", err)
	}
	if !exists {
		return setSchemaVersion(ctx, db, 19)
	}
	hasUserID, err := tableHasColumn(ctx, db, "projects", "user_id")
	if err != nil {
		return fmt.Errorf("check projects.user_id column: %w", err)
	}
	if !hasUserID {
		if _, err := db.ExecContext(ctx, `ALTER TABLE projects ADD COLUMN user_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add projects.user_id column: %w", err)
		}
	}
	return setSchemaVersion(ctx, db, 19)
}
