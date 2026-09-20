package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

type failUpdateResultStore struct {
	*testutil.FakeKanbanStore
}

func (s *failUpdateResultStore) UpdateTaskResult(context.Context, string, time.Time, models.TaskResult) (*models.Task, error) {
	return nil, models.ErrStateConflict
}

type failReviewUsedMarkerStore struct {
	*testutil.FakeKanbanStore
}

func (s *failReviewUsedMarkerStore) AddComment(ctx context.Context, c models.Comment) error {
	if strings.HasPrefix(c.Body, hitlReviewUsedPrefix) {
		return errors.New("marker write failed")
	}
	return s.FakeKanbanStore.AddComment(ctx, c)
}

func TestTryFinalizeApprovedReview_MarkUsedFailureStillSucceeds(t *testing.T) {
	t.Parallel()
	base := testutil.NewFakeStore()
	store := &failReviewUsedMarkerStore{FakeKanbanStore: base}
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "finalize-marker-fail",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	running, err := store.MarkTaskRunning(ctx, parent.ID, parent.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	w := &Worker{store: store}
	w.createReviewHandoff(ctx, *running, "approved draft output")
	completeReviewSubtask(t, store, ctx, parent.ID)

	done, err := w.tryFinalizeApprovedReview(ctx, *running)
	if err != nil {
		t.Fatalf("tryFinalizeApprovedReview: %v", err)
	}
	if !done {
		t.Fatal("expected done=true when commit succeeds despite marker failure")
	}
	final, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get final parent: %v", err)
	}
	if final.State != models.TaskStateCompleted {
		t.Fatalf("parent state = %s, want COMPLETED", final.State)
	}
	comments, err := store.ListComments(ctx, parent.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	review := findReviewSubtask(t, store, ctx, parent.ID)
	if isReviewConsumed(comments, review.ID) {
		t.Fatal("review-used marker should not be written when marker AddComment fails")
	}
}

func TestTryFinalizeApprovedReview_CommitFailureLeavesMarkerAbsent(t *testing.T) {
	t.Parallel()
	base := testutil.NewFakeStore()
	store := &failUpdateResultStore{FakeKanbanStore: base}
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "finalize-fail",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	running, err := store.MarkTaskRunning(ctx, parent.ID, parent.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	w := &Worker{store: store}
	w.createReviewHandoff(ctx, *running, "approved draft output")
	completeReviewSubtask(t, store, ctx, parent.ID)

	done, err := w.tryFinalizeApprovedReview(ctx, *running)
	if err != nil {
		t.Fatalf("tryFinalizeApprovedReview: %v", err)
	}
	if done {
		t.Fatal("expected done=false when commit fails")
	}
	comments, err := store.ListComments(ctx, parent.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	review := findReviewSubtask(t, store, ctx, parent.ID)
	if isReviewConsumed(comments, review.ID) {
		t.Fatal("review-used marker should not be written when commit fails")
	}
}
