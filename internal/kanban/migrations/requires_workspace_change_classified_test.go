package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationAddsRequiresWorkspaceChangeClassifiedColumn(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v30?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v7SchemaSQL); err != nil {
		t.Fatalf("create v7 schema: %v", err)
	}
	if err := Run(ctx, db, t.TempDir()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var value int
	if err := db.QueryRowContext(ctx, `SELECT requires_workspace_change_classified FROM tasks WHERE id = 'task'`).Scan(&value); err != nil {
		t.Fatalf("read requires_workspace_change_classified: %v", err)
	}
	if value != 0 {
		t.Fatalf("requires_workspace_change_classified = %d, want 0 for an existing task", value)
	}
}
