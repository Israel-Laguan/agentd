package testutil

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestFakeKanbanStore_BlockedParentStaysBlockedWhenNonHITLChildFails(t *testing.T) {
	ctx := context.Background()
	store := NewFakeStore()
	parent := seedRunningParent(t, store, ctx, "parent-non-hitl-fail")

	blocked, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title: "worker breakdown child",
	}})
	if err != nil {
		t.Fatalf("BlockTaskWithSubtasks: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, children[0].ID, children[0].UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail child: %v", err)
	}
	parentAfter, err := store.GetTask(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parentAfter.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", parentAfter.State)
	}
}

func TestFakeKanbanStore_BlockedParentResumesAfterHITLChildFailed(t *testing.T) {
	ctx := context.Background()
	store := NewFakeStore()
	parent := seedRunningParent(t, store, ctx, "parent-hitl-fail")

	blocked, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleApproveTool + "deploy",
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("BlockTaskWithSubtasks: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, children[0].ID, children[0].UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail child: %v", err)
	}
	parentAfter, err := store.GetTask(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parentAfter.State != models.TaskStateReady {
		t.Fatalf("parent state = %s, want READY", parentAfter.State)
	}
}

func TestFakeKanbanStore_TerminalStatesSetCompletedAt(t *testing.T) {
	ctx := context.Background()
	store := NewFakeStore()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "completed-at-state",
		Tasks:       []models.DraftTask{{Title: "task-state", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]
	running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	completed, err := store.UpdateTaskState(ctx, running.ID, running.UpdatedAt, models.TaskStateCompleted)
	if err != nil {
		t.Fatalf("complete via UpdateTaskState: %v", err)
	}
	if completed.CompletedAt == nil {
		t.Fatal("UpdateTaskState COMPLETED: CompletedAt is nil, want set")
	}

	_, tasks, err = store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "completed-at-result",
		Tasks:       []models.DraftTask{{Title: "task-result", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task = tasks[0]
	running, err = store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	finished, err := store.UpdateTaskResult(ctx, running.ID, running.UpdatedAt, models.TaskResult{Success: true})
	if err != nil {
		t.Fatalf("complete via UpdateTaskResult: %v", err)
	}
	if finished.CompletedAt == nil {
		t.Fatal("UpdateTaskResult success: CompletedAt is nil, want set")
	}

	_, tasks, err = store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "completed-at-clear",
		Tasks:       []models.DraftTask{{Title: "task-clear", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task = tasks[0]
	done, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateCompleted)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	ready, err := store.UpdateTaskState(ctx, done.ID, done.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("requeue task: %v", err)
	}
	if ready.CompletedAt != nil {
		t.Fatalf("non-terminal UpdateTaskState: CompletedAt = %v, want nil", ready.CompletedAt)
	}
}

func seedRunningParent(t *testing.T, store *FakeKanbanStore, ctx context.Context, name string) models.Task {
	t.Helper()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: name,
		Tasks:       []models.DraftTask{{Title: name + "-task", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	running, err := store.MarkTaskRunning(ctx, parent.ID, parent.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	return *running
}
