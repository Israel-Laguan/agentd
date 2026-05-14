package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestAddCommentUserMovesTaskToInConsideration(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v len=%d", err, len(tasks))
	}
	task := tasks[0]
	if err := store.AddComment(ctx, models.Comment{
		TaskID: task.ID,
		Author: models.CommentAuthorUser,
		Body:   "please review",
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	updated, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.State != models.TaskStateInConsideration {
		t.Fatalf("state = %s want IN_CONSIDERATION", updated.State)
	}
	if updated.Assignee != models.TaskAssigneeHuman {
		t.Fatalf("assignee = %s", updated.Assignee)
	}
}

func TestAddCommentAndPauseNormalizesContent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	if err := store.AddCommentAndPause(ctx, task.ID, models.Comment{
		Content: "  pause me  ",
	}); err != nil {
		t.Fatalf("AddCommentAndPause: %v", err)
	}
}

func TestListCommentsAndEventsRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	if err := store.AddComment(ctx, models.Comment{
		TaskID: task.ID,
		Author: models.CommentAuthorFrontdesk,
		Body:   "note",
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	comments, err := store.ListComments(ctx, task.ID)
	if err != nil || len(comments) < 1 {
		t.Fatalf("ListComments: %v %#v", err, comments)
	}
	evs, err := store.ListEventsByTask(ctx, task.ID)
	if err != nil || len(evs) < 1 {
		t.Fatalf("ListEventsByTask: %v", err)
	}
	if err := store.MarkEventsCurated(ctx, task.ID); err != nil {
		t.Fatalf("MarkEventsCurated: %v", err)
	}
	comments, err = store.ListComments(ctx, task.ID)
	if err != nil || len(comments) != 0 {
		t.Fatalf("ListComments after curation: %v %#v", err, comments)
	}
	comments, err = store.ListCommentsSince(ctx, task.ID, time.Time{})
	if err != nil || len(comments) != 0 {
		t.Fatalf("ListCommentsSince after curation: %v %#v", err, comments)
	}
	if err := store.DeleteCuratedEvents(ctx, task.ID); err != nil {
		t.Fatalf("DeleteCuratedEvents: %v", err)
	}
}

func TestAppendEventNormalizeAndReject(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	proj, _, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "e", Tasks: []models.DraftTask{{Title: "t"}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := store.AppendEvent(ctx, models.Event{ProjectID: "", Type: models.EventTypeComment}); err == nil {
		t.Fatal("expected error for empty project id")
	}
	if err := store.AppendEvent(ctx, models.Event{ProjectID: proj.ID, Type: ""}); err == nil {
		t.Fatal("expected error for empty type")
	}
	if err := store.AppendEvent(ctx, models.Event{
		ProjectID: proj.ID,
		Type:      models.EventTypeComment,
		Payload:   "p",
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
}

func TestListCompletedTasksOlderThanQuery(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, _, err := store.MaterializePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v %#v", err, claimed)
	}
	running, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, 4242)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	completeTask(t, store, ctx, *running)
	if _, err := store.ListCompletedTasksOlderThan(ctx, 48*time.Hour); err != nil {
		t.Fatalf("ListCompletedTasksOlderThan: %v", err)
	}
}
