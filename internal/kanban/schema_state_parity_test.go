package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"

	_ "modernc.org/sqlite"
)

// TestSchemaAcceptsEveryValidTaskState proves the DB CHECK constraint on
// tasks.state accepts every state models.TaskState.Valid() reports true
// for. This guards against the drift that let NEEDS_CONTEXT sit in the Go
// enum/transition table for a full sprint while the DB constraint silently
// rejected it (see S04 retro / T-020).
func TestSchemaAcceptsEveryValidTaskState(t *testing.T) {
	db, err := Open("file:schema-state-parity?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	now := formatTime(time.Now().UTC())
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES ('parity-project', 'Project', 'input', 'workspace', 'ACTIVE', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	states := models.AllTaskStatesSlice()
	if len(states) == 0 {
		t.Fatal("models.AllTaskStatesSlice() is empty")
	}
	// Assert the literal covers every models.AllTaskStates member so
	// the two cannot silently diverge.
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			if !state.Valid() {
				t.Fatalf("%q is not Valid() per the Go enum", state)
			}
			id := "parity-" + string(state)
			if _, err := db.ExecContext(ctx, `
				INSERT INTO tasks (id, project_id, agent_id, title, description, state, assignee, created_at, updated_at)
				VALUES (?, 'parity-project', 'default', 'T', 'd', ?, 'SYSTEM', ?, ?)`,
				id, string(state), now, now); err != nil {
				t.Fatalf("insert task with state %q rejected by DB CHECK constraint: %v", state, err)
			}
		})
	}
}
