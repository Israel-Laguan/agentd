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

// setupReviewProfile enables RequireReview on the default profile. agentic picks
// the engine: the legacy-only assertions in this file keep the legacy engine.
func setupReviewProfile(t *testing.T, store models.KanbanStore, ctx context.Context, provider string, agentic bool) {
	t.Helper()
	profile, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profile.RequireReview = true
	profile.AgenticMode = agentic
	if provider != "" {
		profile.Provider = provider
	}
	if err := store.UpsertAgentProfile(ctx, *profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
}

func setupAgenticReviewProfile(t *testing.T, store models.KanbanStore, ctx context.Context) {
	t.Helper()
	profile, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profile.RequireReview = true
	profile.AgenticMode = true
	profile.Provider = "openai"
	if err := store.UpsertAgentProfile(ctx, *profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
}

func TestProcess_AgenticRequireReview_CreatesReviewHandoff(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	setupAgenticReviewProfile(t, store, ctx)

	gw := &sequenceGateway{
		responses: []gateway.AIResponse{
			{Content: "agentic draft output for human review"},
		},
	}
	sb := &mockAgenticSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	task := materializeLegacyReviewTask(t, store, ctx, "agentic-review", "task-agentic-review")
	queued, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("queue task: %v", err)
	}

	w.Process(ctx, *queued)

	parent, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", parent.State)
	}
	if sb.executionCount != 0 {
		t.Fatalf("sandbox executions = %d, want 0 (final text only)", sb.executionCount)
	}
	if gw.callCount != 1 {
		t.Fatalf("gateway calls = %d, want 1", gw.callCount)
	}
	draft, ok := findLatestDraftReviewFromStore(t, store, ctx, task.ID)
	if !ok || !strings.Contains(draft, "agentic draft output") {
		t.Fatalf("draft = %q, want agentic final text", draft)
	}
	_ = findReviewSubtask(t, store, ctx, task.ID)
}

func findLatestDraftReviewFromStore(t *testing.T, store models.KanbanStore, ctx context.Context, taskID string) (string, bool) {
	t.Helper()
	comments, err := store.ListComments(ctx, taskID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	return findLatestDraftReview(comments)
}

func materializeLegacyReviewTask(t *testing.T, store models.KanbanStore, ctx context.Context, project, title string) models.Task {
	t.Helper()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: project, Tasks: []models.DraftTask{{Title: title, Description: testutil.AgenticTestTaskDescription()}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	return tasks[0]
}

func requeueParentTask(t *testing.T, store models.KanbanStore, ctx context.Context, taskID string) models.Task {
	t.Helper()
	parent, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	requeued, err := store.UpdateTaskState(ctx, parent.ID, parent.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("requeue parent: %v", err)
	}
	return *requeued
}

func TestProcess_RequireReview_FinalizesApprovedReview(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	setupReviewProfile(t, store, ctx, "", true)
	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	task := materializeLegacyReviewTask(t, store, ctx, "legacy-review-finalize", "task-review-finalize")
	w.createReviewHandoff(ctx, task, "approved draft output")
	completeReviewSubtask(t, store, ctx, task.ID)
	queued := requeueParentTask(t, store, ctx, task.ID)
	w.Process(ctx, queued)
	final, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get final parent: %v", err)
	}
	if final.State != models.TaskStateCompleted {
		t.Fatalf("parent state = %s, want COMPLETED", final.State)
	}
	if sb.execCount != 0 || len(gw.requests) != 0 {
		t.Fatalf("sandbox=%d gateway=%d, want no re-run", sb.execCount, len(gw.requests))
	}
}

func failReviewWithComment(t *testing.T, store models.KanbanStore, ctx context.Context, parentID, comment string) {
	t.Helper()
	review := findReviewSubtask(t, store, ctx, parentID)
	if err := store.AddComment(ctx, models.Comment{TaskID: review.ID, Author: models.CommentAuthorUser, Body: comment}); err != nil {
		t.Fatalf("add rejection comment: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail review subtask: %v", err)
	}
}

func findReviewSubtask(t *testing.T, store models.KanbanStore, ctx context.Context, parentID string) models.Task {
	t.Helper()
	children, err := store.ListChildTasks(ctx, parentID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	for _, child := range children {
		if strings.HasPrefix(child.Title, models.HITLSubtaskTitleReview) {
			return child
		}
	}
	t.Fatal("expected review subtask")
	return models.Task{}
}

// bumpOnCommentStore bumps the parent task's UpdatedAt on AddComment to simulate
// store side effects between markReviewUsed and commit.
type bumpOnCommentStore struct {
	*testutil.FakeKanbanStore
	bumpParentID string
}

func (s *bumpOnCommentStore) AddComment(ctx context.Context, c models.Comment) error {
	if err := s.FakeKanbanStore.AddComment(ctx, c); err != nil {
		return err
	}
	if c.TaskID != s.bumpParentID {
		return nil
	}
	_, err := s.IncrementRetryCount(ctx, c.TaskID, time.Time{})
	return err
}

// strictUpdateResultStore enforces optimistic locking on UpdateTaskResult.
type strictUpdateResultStore struct {
	*bumpOnCommentStore
	capturedExpected time.Time
}

func (s *strictUpdateResultStore) UpdateTaskResult(ctx context.Context, id string, expected time.Time, result models.TaskResult) (*models.Task, error) {
	s.capturedExpected = expected
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if !task.UpdatedAt.Equal(expected) {
		return nil, models.ErrStateConflict
	}
	return s.bumpOnCommentStore.UpdateTaskResult(ctx, id, expected, result)
}

func TestTryFinalizeApprovedReview_RefreshesTaskBeforeCommit(t *testing.T) {
	t.Parallel()
	store, w, parent := setupFinalizeReviewRefreshFixture(t)
	ctx := context.Background()
	completeReviewSubtask(t, store, ctx, parent.ID)

	current, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	stale := *current
	stale.UpdatedAt = current.UpdatedAt.Add(-time.Hour)

	done, err := w.tryFinalizeApprovedReview(ctx, stale)
	if err != nil {
		t.Fatalf("tryFinalizeApprovedReview: %v", err)
	}
	if !done {
		t.Fatal("expected finalization to complete")
	}
	if store.capturedExpected.Equal(stale.UpdatedAt) {
		t.Fatalf("UpdateTaskResult used stale UpdatedAt %v", stale.UpdatedAt)
	}
	final, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get final parent: %v", err)
	}
	if final.State != models.TaskStateCompleted {
		t.Fatalf("parent state = %s, want COMPLETED", final.State)
	}
}

func setupFinalizeReviewRefreshFixture(t *testing.T) (*strictUpdateResultStore, *Worker, models.Task) {
	t.Helper()
	base := testutil.NewFakeStore()
	store := &strictUpdateResultStore{bumpOnCommentStore: &bumpOnCommentStore{FakeKanbanStore: base}}
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "finalize-refresh",
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
	store.bumpParentID = parent.ID
	w := &Worker{store: store}
	w.createReviewHandoff(ctx, *running, "approved draft output")
	return store, w, parent
}

func completeReviewSubtask(t *testing.T, store models.KanbanStore, ctx context.Context, parentID string) {
	t.Helper()
	review := findReviewSubtask(t, store, ctx, parentID)
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete review: %v", err)
	}
}
