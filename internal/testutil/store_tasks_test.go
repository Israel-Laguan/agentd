package testutil

import (
	"context"
	"testing"
	"time"

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

func TestFakeKanbanStore_BlockTaskWithSubtasksAndCommentsSuccess(t *testing.T) {
	ctx := context.Background()
	store := NewFakeStore()
	parent := seedRunningParent(t, store, ctx, "parent-atomic-comments")

	blocked, children, err := store.BlockTaskWithSubtasksAndComments(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title: "clarify",
	}}, []models.Comment{
		{Author: models.CommentAuthorWorkerAgent, Body: "agentd:hitl:elicitation-questions:[]"},
		{Author: models.CommentAuthorWorkerAgent, Body: models.HITLExpiresAtCommentPrefix + time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("BlockTaskWithSubtasksAndComments: %v", err)
	}
	if blocked.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", blocked.State)
	}
	if len(children) != 1 || children[0].Title != "clarify" {
		t.Fatalf("children = %#v, want one child", children)
	}
	comments, err := store.ListComments(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(comments))
	}
	if comments[0].Author != models.CommentAuthorWorkerAgent || comments[1].Author != models.CommentAuthorWorkerAgent {
		t.Fatalf("comment authors = %s, %s; want WORKER_AGENT", comments[0].Author, comments[1].Author)
	}
	if !comments[1].CreatedAt.After(comments[0].CreatedAt) {
		t.Fatalf("comment order = %s, %s; want expiry after questions", comments[0].CreatedAt, comments[1].CreatedAt)
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

	t.Run("UpdateTaskState_completed", func(t *testing.T) {
		task := seedMaterializedTask(t, store, ctx, "completed-at-state", "task-state")
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
	})

	t.Run("UpdateTaskResult_success", func(t *testing.T) {
		task := seedMaterializedTask(t, store, ctx, "completed-at-result", "task-result")
		running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 1)
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
	})

	t.Run("non_terminal_clears_completed_at", func(t *testing.T) {
		task := seedMaterializedTask(t, store, ctx, "completed-at-clear", "task-clear")
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
	})
}

func seedMaterializedTask(t *testing.T, store *FakeKanbanStore, ctx context.Context, projectName, title string) models.Task {
	t.Helper()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: projectName,
		Tasks:       []models.DraftTask{{Title: title, Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	return tasks[0]
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
