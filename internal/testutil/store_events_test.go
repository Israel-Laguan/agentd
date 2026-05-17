package testutil

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestFakeKanbanStore_CommentPayloadRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewFakeStore()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "comment-roundtrip",
		Tasks:       []models.DraftTask{{Title: "t1"}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	taskID := tasks[0].ID

	body := "agentd:hitl:draft-review\nclean output\n"
	if err := store.AddComment(ctx, models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   body,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	comments, err := store.ListComments(ctx, taskID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("ListComments: %v len=%d", err, len(comments))
	}
	got := comments[0]
	if got.Author != models.CommentAuthorWorkerAgent {
		t.Fatalf("author = %q, want WORKER_AGENT", got.Author)
	}
	wantBody := "agentd:hitl:draft-review\nclean output"
	if got.Body != wantBody {
		t.Fatalf("body = %q, want %q (trailing whitespace stripped like kanban)", got.Body, wantBody)
	}
}
