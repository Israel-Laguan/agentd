package worker

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func setupConsumeElicitationFixture(t *testing.T) (
	context.Context, *testutil.FakeKanbanStore, *Worker, *models.Task, *models.Task,
) {
	t.Helper()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "consume-proj",
		Tasks:       []models.DraftTask{{Title: "Fix", Description: "fix the bug"}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	parent := &tasks[0]
	running, err := store.MarkTaskRunning(ctx, parent.ID, parent.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}

	questions := []ElicitationQuestion{
		{Question: "Which bug?"},
		{Question: "Which environment?"},
	}
	if err := recordElicitationQuestions(ctx, store, parent.ID, questions); err != nil {
		t.Fatalf("record questions: %v", err)
	}

	_, children, err := store.BlockTaskWithSubtasks(ctx, running.ID, running.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleClarification + "Which bug?",
		Description: "clarify",
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	child := &children[0]
	if err := store.AddComment(ctx, models.Comment{
		TaskID: child.ID,
		Author: models.CommentAuthorUser,
		Body:   "Login timeout on /auth in staging.",
	}); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, child.ID, child.UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete child: %v", err)
	}

	w := &Worker{store: store}
	return ctx, store, w, parent, running
}
