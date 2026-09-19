package kanban

import (
	"testing"
	"time"

	"agentd/internal/models"

	_ "modernc.org/sqlite"
)

// TestTaskStateCheckConstraintParity asserts that every state models.TaskState
// considers valid is also accepted by the tasks.state CHECK constraint.
//
// S04 shipped NEEDS_CONTEXT in the Go enum and the transition table but not in
// the CHECK, so the state machine advertised a transition that failed at the
// DB layer. This test is the S04 retro action that stops that drift recurring:
// adding a state to models.AllTaskStates without a matching migration fails here.
func TestTaskStateCheckConstraintParity(t *testing.T) {
	db, err := Open("file:task-state-parity?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES ('parity-proj', 'parity', 'parity', '/tmp/parity-ws', 'ACTIVE', ?, ?)`,
		now, now); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	if len(models.AllTaskStates) == 0 {
		t.Fatal("models.AllTaskStates is empty")
	}
	for _, state := range models.AllTaskStates {
		t.Run(string(state), func(t *testing.T) {
			if !state.Valid() {
				t.Fatalf("state %q is in AllTaskStates but Valid() = false", state)
			}
			_, err := db.Exec(`
				INSERT INTO tasks (id, project_id, agent_id, title, description, state, assignee, created_at, updated_at)
				VALUES (?, 'parity-proj', 'default', ?, 'parity probe', ?, 'SYSTEM', ?, ?)`,
				"parity-"+string(state), "parity "+string(state), string(state), now, now)
			if err != nil {
				t.Fatalf("tasks.state CHECK rejects %q, which models.TaskState accepts: %v\n"+
					"add the state to the schema CHECK and ship a migration for existing databases", state, err)
			}
		})
	}
}

// TestTaskStateCheckRejectsUnknownState is the other half of the parity
// guarantee: the CHECK must not be broader than the Go enum either.
func TestTaskStateCheckRejectsUnknownState(t *testing.T) {
	db, err := Open("file:task-state-parity-unknown?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES ('unknown-proj', 'unknown', 'unknown', '/tmp/unknown-ws', 'ACTIVE', ?, ?)`,
		now, now); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO tasks (id, project_id, agent_id, title, description, state, assignee, created_at, updated_at)
		VALUES ('unknown-1', 'unknown-proj', 'default', 'bogus', 'bogus', 'NOT_A_STATE', 'SYSTEM', ?, ?)`,
		now, now)
	if err == nil {
		t.Fatal("tasks.state CHECK accepted 'NOT_A_STATE'; the constraint is broader than models.TaskState")
	}
}
