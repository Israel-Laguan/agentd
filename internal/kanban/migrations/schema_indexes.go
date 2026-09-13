package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

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
