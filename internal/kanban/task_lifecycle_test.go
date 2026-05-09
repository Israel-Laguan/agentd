package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestMarkTaskRunningAndRetry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "lifecycle", Tasks: []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, 1234)
	if err != nil {
		t.Fatal(err)
	}
	if running.State != models.TaskStateRunning || running.OSProcessID == nil || *running.OSProcessID != 1234 {
		t.Fatalf("running task = %#v", running)
	}
	retried, err := store.IncrementRetryCount(ctx, tasks[0].ID, running.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if retried.RetryCount != 1 {
		t.Fatalf("retry_count = %d, want 1", retried.RetryCount)
	}
}

func TestAppendTasksToProjectAndCommentIntake(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "intake", Tasks: []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = store.AddComment(ctx, models.Comment{TaskID: tasks[0].ID, Author: "Human", Body: "add follow ups"})
	if err != nil {
		t.Fatal(err)
	}
	refs := requireUnprocessedComments(t, store, ctx, 1)
	added, err := store.AppendTasksToProject(ctx, project.ID, tasks[0].ID, []models.DraftTask{{Title: "B"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].State != models.TaskStatePending {
		t.Fatalf("added = %#v", added)
	}
	if err := store.MarkCommentProcessed(ctx, tasks[0].ID, refs[0].CommentEventID); err != nil {
		t.Fatal(err)
	}
	requireUnprocessedComments(t, store, ctx, 0)
}

func requireUnprocessedComments(
	t *testing.T,
	store *Store,
	ctx context.Context,
	want int,
) []models.CommentRef {
	t.Helper()
	refs, err := store.ListUnprocessedHumanComments(ctx)
	if err != nil || len(refs) != want {
		t.Fatalf("refs=%#v err=%v want=%d", refs, err, want)
	}
	return refs
}
