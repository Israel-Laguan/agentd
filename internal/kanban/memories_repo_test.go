package kanban

import (
	"context"
	"database/sql"
	"testing"

	"agentd/internal/models"
)

func TestRecordAndListMemories(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject() error = %v", err)
	}
	if err := recordGlobalMemory(ctx, store); err != nil {
		t.Fatalf("RecordMemory(global) error = %v", err)
	}
	if err := recordProjectMemory(ctx, store, project.ID); err != nil {
		t.Fatalf("RecordMemory(project) error = %v", err)
	}
	assertGlobalMemory(t, ctx, store)
	assertProjectMemory(t, ctx, store, project.ID)
}

func recordGlobalMemory(ctx context.Context, store *Store) error {
	return store.RecordMemory(ctx, models.Memory{
		Scope: "GLOBAL",
		Tags: sql.NullString{
			String: "reboot,recovery",
			Valid:  true,
		},
		Symptom: sql.NullString{
			String: "daemon_reboot_interrupted_tasks",
			Valid:  true,
		},
		Solution: sql.NullString{
			String: "task was reset after daemon startup",
			Valid:  true,
		},
	})
}

func recordProjectMemory(ctx context.Context, store *Store, projectID string) error {
	return store.RecordMemory(ctx, models.Memory{
		Scope: "PROJECT",
		ProjectID: sql.NullString{
			String: projectID,
			Valid:  true,
		},
		Symptom: sql.NullString{
			String: "project-specific symptom",
			Valid:  true,
		},
	})
}

func assertGlobalMemory(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	global, err := store.ListMemories(ctx, models.MemoryFilter{Scope: "GLOBAL"})
	if err != nil {
		t.Fatalf("ListMemories(global) error = %v", err)
	}
	if len(global) != 1 || global[0].Symptom.String != "daemon_reboot_interrupted_tasks" {
		t.Fatalf("global memories = %#v, want reboot memory", global)
	}
}

func assertProjectMemory(t *testing.T, ctx context.Context, store *Store, projectID string) {
	t.Helper()
	projectMemories, err := store.ListMemories(ctx, models.MemoryFilter{
		Scope: "PROJECT",
		ProjectID: sql.NullString{
			String: projectID,
			Valid:  true,
		},
	})
	if err != nil {
		t.Fatalf("ListMemories(project) error = %v", err)
	}
	if len(projectMemories) != 1 || projectMemories[0].ProjectID.String != projectID {
		t.Fatalf("project memories = %#v, want one project-scoped memory", projectMemories)
	}
}
