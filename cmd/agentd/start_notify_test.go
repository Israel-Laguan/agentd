package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/bus"
	"agentd/internal/kanban"
	"agentd/internal/models"
)

// B-074: the daemon's store announces committed task changes on the bus, so a
// dispatch claim reaches SSE subscribers without the dispatcher publishing.
func TestWithLiveTaskNotifierPublishesStoreChanges(t *testing.T) {
	dir := t.TempDir()
	base, err := kanban.OpenStore(filepath.Join(dir, "agentd.db"), filepath.Join(dir, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	eventBus := bus.NewInProcess()
	store := withLiveTaskNotifier(base, eventBus)
	ctx := context.Background()
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "notify", Tasks: []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	frames, cancel := eventBus.Subscribe(bus.GlobalTopic, 8)
	defer cancel()

	if _, err := store.ClaimNextReadyTasks(ctx, 1); err != nil {
		t.Fatal(err)
	}

	select {
	case sig := <-frames:
		if sig.Type != "task_updated" || sig.TaskID != tasks[0].ID || sig.ProjectID != project.ID {
			t.Fatalf("signal = %+v, want task_updated for %s in %s", sig, tasks[0].ID, project.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("claim published no frame")
	}
}
