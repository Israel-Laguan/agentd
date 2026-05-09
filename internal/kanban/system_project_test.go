package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestEnsureSystemProjectIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject() error = %v", err)
	}
	second, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject() second error = %v", err)
	}
	if first.ID != second.ID || first.Name != "_system" {
		t.Fatalf("system project mismatch: first=%#v second=%#v", first, second)
	}
	assertCount(t, store.db, "projects", 1)
}

func TestEnsureProjectTaskDeduplicatesOpenTask(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject() error = %v", err)
	}

	draft := models.DraftTask{Title: "System Offline: Please check AI API connections.", Description: "debug", Assignee: models.TaskAssigneeHuman}
	first, created, err := store.EnsureProjectTask(ctx, project.ID, draft)
	if err != nil {
		t.Fatalf("EnsureProjectTask() error = %v", err)
	}
	if !created {
		t.Fatal("expected first task to be created")
	}
	second, created, err := store.EnsureProjectTask(ctx, project.ID, draft)
	if err != nil {
		t.Fatalf("EnsureProjectTask() second error = %v", err)
	}
	if created {
		t.Fatal("expected second task to be deduplicated")
	}
	if first.ID != second.ID {
		t.Fatalf("task ID = %s, want %s", second.ID, first.ID)
	}
	assertCount(t, store.db, "tasks", 1)
}
