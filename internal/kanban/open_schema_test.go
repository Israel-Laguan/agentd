package kanban

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenBootstrapsCurrentSchemaVersion(t *testing.T) {
	db, err := Open("file:migrate-current-version?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var version string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "8" {
		t.Fatalf("schema version = %q, want 8", version)
	}

	var successCriteria string
	err = db.QueryRow(`SELECT success_criteria FROM tasks LIMIT 0`).Scan(&successCriteria)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query success_criteria column: %v", err)
	}
}

func TestSchemaAllowsDependsOnTaskRelations(t *testing.T) {
	db, err := Open("file:migrate-depends-on?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	now := formatTime(time.Now().UTC())
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES ('project', 'Project', 'input', 'workspace', 'ACTIVE', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (id, project_id, agent_id, title, description, state, assignee, created_at, updated_at)
		VALUES
			('parent', 'project', 'default', 'Parent', 'parent desc', 'READY', 'SYSTEM', ?, ?),
			('child', 'project', 'default', 'Child', 'child desc', 'PENDING', 'SYSTEM', ?, ?)`,
		now, now, now, now); err != nil {
		t.Fatalf("insert tasks: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO task_relations (parent_task_id, child_task_id, relation_type)
		VALUES ('parent', 'child', 'DEPENDS_ON')`); err != nil {
		t.Fatalf("insert DEPENDS_ON relation: %v", err)
	}
}
