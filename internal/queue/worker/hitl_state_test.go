package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestFindLatestApprovalSubtask_ExactTitleOnly(t *testing.T) {
	t.Parallel()
	older := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	children := []models.Task{
		{
			BaseEntity: models.BaseEntity{ID: "deploy-prod", UpdatedAt: newer},
			Title:      approvalSubtaskTitle("deploy-prod"),
			State:      models.TaskStateCompleted,
		},
		{
			BaseEntity: models.BaseEntity{ID: "deploy", UpdatedAt: older},
			Title:      approvalSubtaskTitle("deploy"),
			State:      models.TaskStateCompleted,
		},
	}
	got := findLatestApprovalSubtask(children, "deploy")
	if got == nil {
		t.Fatal("expected deploy approval subtask")
	}
	if got.ID != "deploy" {
		t.Fatalf("got subtask %q, want deploy (not deploy-prod prefix match)", got.ID)
	}
}

func TestPrependReviewRejectionFeedback_InjectsOnce(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "review-reject",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]

	_, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleReview + " draft output pending approval",
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	review := children[0]
	if err := store.AddComment(ctx, models.Comment{
		TaskID: review.ID,
		Author: models.CommentAuthorUser,
		Body:   "Please add error handling",
	}); err != nil {
		t.Fatalf("add rejection comment: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail review subtask: %v", err)
	}

	w := &Worker{store: store}
	base := []gateway.PromptMessage{{Role: "system", Content: "sys"}, {Role: "user", Content: "task"}}

	first := w.prependReviewRejectionFeedback(ctx, parent, base)
	if len(first) != len(base)+1 {
		t.Fatalf("messages = %d, want %d", len(first), len(base)+1)
	}
	if !strings.Contains(first[len(first)-1].Content, "Please add error handling") {
		t.Fatalf("feedback = %q, want rejection reason", first[len(first)-1].Content)
	}

	second := w.prependReviewRejectionFeedback(ctx, parent, base)
	if len(second) != len(base) {
		t.Fatalf("second call messages = %d, want %d (rejection already consumed)", len(second), len(base))
	}
}

func TestCreatePromptHandoff_RecordsLegacyExpiry(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	w := NewWorker(store, nil, nil, nil, sink, WorkerOptions{})
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "prompt-expiry",
		Tasks:       []models.DraftTask{{Title: "task-prompt", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	before := time.Now()
	w.createPromptHandoff(ctx, task, "interactive prompt detected")
	after := time.Now()

	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	var expiry time.Time
	var found bool
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, models.HITLExpiresAtCommentPrefix) {
			continue
		}
		raw := strings.TrimPrefix(c.Body, models.HITLExpiresAtCommentPrefix)
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		expiry = parsed
		found = true
		break
	}
	if !found {
		t.Fatal("expected legacy handoff expiry comment")
	}
	expiry = expiry.UTC().Truncate(time.Second)
	wantMin := before.Add(w.legacyHandoffTimeout).UTC().Truncate(time.Second)
	wantMax := after.Add(w.legacyHandoffTimeout).UTC().Truncate(time.Second)
	if expiry.Before(wantMin) || expiry.After(wantMax) {
		t.Fatalf("expiry = %s, want in [%s, %s]", expiry, wantMin, wantMax)
	}
}

type blockFailStore struct {
	*testutil.FakeKanbanStore
}

func (s *blockFailStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, _ []models.DraftTask) (*models.Task, []models.Task, error) {
	return nil, nil, models.ErrStateConflict
}

func TestCreatePromptHandoff_NoExpiryWhenBlockFails(t *testing.T) {
	t.Parallel()
	store := &blockFailStore{FakeKanbanStore: testutil.NewFakeStore()}
	sink := &mockEventSink{}
	w := NewWorker(store, nil, nil, nil, sink, WorkerOptions{})
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "prompt-block-fail",
		Tasks:       []models.DraftTask{{Title: "task-prompt-fail", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	w.createPromptHandoff(ctx, task, "interactive prompt detected")

	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	for _, c := range comments {
		if strings.HasPrefix(c.Body, models.HITLExpiresAtCommentPrefix) {
			t.Fatalf("unexpected expiry comment when block failed: %q", c.Body)
		}
	}
}
