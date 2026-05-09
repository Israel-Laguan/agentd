package queue

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestBootReconcileUsesPIDProbe(t *testing.T) {
	store := newWorkerStore()
	err := BootReconcile(context.Background(), store, StaticPIDProbe{PIDs: []int{1, 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBootReconcileCreatesReviewTaskAndMemory(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	seedInterruptedTask(t, ctx, store)
	sink := &recordingSink{}

	if err := BootReconcile(ctx, store, StaticPIDProbe{PIDs: []int{1, 2}}, sink); err != nil {
		t.Fatalf("bootReconcile() error = %v", err)
	}
	systemID := mustSystemProjectID(t, ctx, store)
	assertRecoveryReviewTask(t, ctx, store, systemID)
	assertRecoveryMemoryAndEvents(t, ctx, store, sink)
}

func seedInterruptedTask(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore) string {
	t.Helper()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "recover",
		Tasks: []models.DraftTask{
			{TempID: "a", Title: "Interrupted task"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != tasks[0].ID {
		t.Fatalf("claimed = %#v, want original task", claimed)
	}
	if _, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, 4242); err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}
	return tasks[0].ID
}

func mustSystemProjectID(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore) string {
	t.Helper()
	systemProjects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	systemID := ""
	for _, project := range systemProjects {
		if project.Name == "_system" {
			systemID = project.ID
			break
		}
	}
	if systemID == "" {
		t.Fatal("system project was not created")
	}
	return systemID
}

func assertRecoveryReviewTask(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, systemID string) {
	t.Helper()
	systemTasks, err := store.ListTasksByProject(ctx, systemID)
	if err != nil {
		t.Fatalf("ListTasksByProject(system) error = %v", err)
	}
	if len(systemTasks) != 1 {
		t.Fatalf("system tasks = %d, want 1", len(systemTasks))
	}
	task := systemTasks[0]
	if task.Assignee != models.TaskAssigneeHuman || !strings.Contains(task.Title, "Daemon Reboot Recovery") {
		t.Fatalf("review task = %#v, want HUMAN reboot recovery task", task)
	}
	if !strings.Contains(task.Description, "Interrupted task") || !strings.Contains(task.Description, "partial filesystem") {
		t.Fatalf("review task description missing recovery context:\n%s", task.Description)
	}
}

func assertRecoveryMemoryAndEvents(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, sink *recordingSink) {
	t.Helper()
	memories, err := store.ListMemories(ctx, models.MemoryFilter{Scope: "GLOBAL"})
	if err != nil {
		t.Fatalf("ListMemories() error = %v", err)
	}
	if len(memories) != 1 || memories[0].Symptom.String != "daemon_reboot_interrupted_tasks" {
		t.Fatalf("memories = %#v, want reboot recovery memory", memories)
	}
	if len(sink.events) != 2 {
		t.Fatalf("sink events = %#v, want recovery and handoff events", sink.events)
	}
	if sink.events[1].Type != RebootRecoveryHandoffEventType {
		t.Fatalf("second event type = %s, want %s", sink.events[1].Type, RebootRecoveryHandoffEventType)
	}
}

func TestBootReconcileSkipsReviewWhenNoGhosts(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	sink := &recordingSink{}

	err := BootReconcile(ctx, store, StaticPIDProbe{PIDs: []int{1, 2}}, sink)
	if err != nil {
		t.Fatalf("bootReconcile() error = %v", err)
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("projects = %#v, want no system project", projects)
	}
	memories, err := store.ListMemories(ctx, models.MemoryFilter{})
	if err != nil {
		t.Fatalf("ListMemories() error = %v", err)
	}
	if len(memories) != 0 || len(sink.events) != 0 {
		t.Fatalf("memories=%#v events=%#v, want none", memories, sink.events)
	}
}
