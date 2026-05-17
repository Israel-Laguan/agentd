package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

type captureResultStore struct {
	*testutil.FakeKanbanStore
	lastResult *models.TaskResult
}

func (s *captureResultStore) UpdateTaskResult(ctx context.Context, id string, expected time.Time, result models.TaskResult) (*models.Task, error) {
	s.lastResult = &result
	return s.FakeKanbanStore.UpdateTaskResult(ctx, id, expected, result)
}

type stdoutReviewSandbox struct {
	execCount int
	result    sandbox.Result
}

func (s *stdoutReviewSandbox) Execute(_ context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	s.execCount++
	return s.result, nil
}

func TestProcess_LegacyRequireReview_DraftAndFinalPayloadUseRawStdout(t *testing.T) {
	t.Parallel()
	store := &captureResultStore{FakeKanbanStore: testutil.NewFakeStore()}
	ctx := context.Background()

	profile, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profile.RequireReview = true
	profile.AgenticMode = false
	if err := store.UpsertAgentProfile(ctx, *profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	sbResult := sandbox.Result{
		Success:  true,
		ExitCode: 0,
		Stdout:   "clean output\n",
		Duration: 1500 * time.Millisecond,
	}
	sb := &stdoutReviewSandbox{result: sbResult}
	w := NewWorker(store, &routingTestGateway{}, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "legacy-review-stdout",
		Tasks:       []models.DraftTask{{Title: "task-review-stdout", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]
	queued, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("queue task: %v", err)
	}

	w.Process(ctx, *queued)

	blocked, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get blocked parent: %v", err)
	}
	if blocked.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED after first Process", blocked.State)
	}
	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	draft, ok := findLatestDraftReview(comments)
	if !ok {
		t.Fatal("expected draft review comment")
	}
	if strings.Contains(draft, "exit=") {
		t.Fatalf("draft should be raw stdout, got %q", draft)
	}
	if !strings.Contains(draft, "clean output") {
		t.Fatalf("draft = %q, want clean sandbox stdout", draft)
	}

	children, err := store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	var review *models.Task
	for i := range children {
		if strings.HasPrefix(children[i].Title, models.HITLSubtaskTitleReview) {
			review = &children[i]
			break
		}
	}
	if review == nil {
		t.Fatal("expected review subtask")
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete review: %v", err)
	}

	parent, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	requeued, err := store.UpdateTaskState(ctx, parent.ID, parent.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("requeue parent: %v", err)
	}

	w.Process(ctx, *requeued)

	final, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get final parent: %v", err)
	}
	if final.State != models.TaskStateCompleted {
		t.Fatalf("parent state = %s, want COMPLETED", final.State)
	}
	if store.lastResult == nil {
		t.Fatal("expected committed task result")
	}
	// Comment round-trip trims trailing whitespace on the stored body (see models.SplitCommentPayload).
	// Approved-review commit uses draft stdout only (no sandbox duration metadata).
	approvedStdout := strings.TrimSpace(sbResult.Stdout)
	wantPayload := resultPayload(sandbox.Result{Success: true, Stdout: approvedStdout})
	if store.lastResult.Payload != wantPayload {
		t.Fatalf("committed payload = %q, want %q", store.lastResult.Payload, wantPayload)
	}
	if strings.Count(store.lastResult.Payload, "exit=") != 1 {
		t.Fatalf("committed payload should have a single metadata prefix, got %q", store.lastResult.Payload)
	}
	if sb.execCount != 1 {
		t.Fatalf("sandbox executions = %d, want 1", sb.execCount)
	}
}

func TestProcess_LegacyRequireReview_FinalizesApprovedReview(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	profile, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profile.RequireReview = true
	profile.AgenticMode = false
	if err := store.UpsertAgentProfile(ctx, *profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "legacy-review-finalize",
		Tasks:       []models.DraftTask{{Title: "task-review-finalize", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	w.createReviewHandoff(ctx, task, "approved draft output")

	children, err := store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	var review *models.Task
	for i := range children {
		if strings.HasPrefix(children[i].Title, models.HITLSubtaskTitleReview) {
			review = &children[i]
			break
		}
	}
	if review == nil {
		t.Fatal("expected review subtask")
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete review: %v", err)
	}

	parent, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	queued, err := store.UpdateTaskState(ctx, parent.ID, parent.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("requeue parent: %v", err)
	}

	w.Process(ctx, *queued)

	final, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get final parent: %v", err)
	}
	if final.State != models.TaskStateCompleted {
		t.Fatalf("parent state = %s, want COMPLETED", final.State)
	}
	if sb.execCount != 0 {
		t.Fatalf("sandbox executions = %d, want 0 (approved review should not re-run command)", sb.execCount)
	}
	if len(gw.requests) != 0 {
		t.Fatalf("gateway requests = %d, want 0", len(gw.requests))
	}
}

func TestProcess_LegacyRequireReview_InjectsRejectionFeedback(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	profile, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profile.RequireReview = true
	profile.AgenticMode = false
	profile.Provider = "ollama"
	if err := store.UpsertAgentProfile(ctx, *profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "legacy-review-reject",
		Tasks:       []models.DraftTask{{Title: "task-review-reject", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	w.createReviewHandoff(ctx, task, "first draft output")

	children, err := store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	var review *models.Task
	for i := range children {
		if strings.HasPrefix(children[i].Title, models.HITLSubtaskTitleReview) {
			review = &children[i]
			break
		}
	}
	if review == nil {
		t.Fatal("expected review subtask")
	}
	const rejectionComment = "Please add error handling"
	if err := store.AddComment(ctx, models.Comment{
		TaskID: review.ID,
		Author: models.CommentAuthorUser,
		Body:   rejectionComment,
	}); err != nil {
		t.Fatalf("add rejection comment: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail review subtask: %v", err)
	}

	parent, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	queued, err := store.UpdateTaskState(ctx, parent.ID, parent.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("requeue parent: %v", err)
	}

	w.Process(ctx, *queued)

	if len(gw.requests) != 1 {
		t.Fatalf("gateway requests = %d, want 1", len(gw.requests))
	}
	var foundFeedback bool
	for _, msg := range gw.requests[0].Messages {
		if strings.Contains(msg.Content, rejectionComment) {
			foundFeedback = true
			break
		}
	}
	if !foundFeedback {
		t.Fatalf("gateway messages missing rejection feedback %q", rejectionComment)
	}
	if sb.execCount != 1 {
		t.Fatalf("sandbox executions = %d, want 1", sb.execCount)
	}
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
	base := testutil.NewFakeStore()
	store := &strictUpdateResultStore{
		bumpOnCommentStore: &bumpOnCommentStore{FakeKanbanStore: base},
	}
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

	children, err := store.ListChildTasks(ctx, parent.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	var review *models.Task
	for i := range children {
		if strings.HasPrefix(children[i].Title, models.HITLSubtaskTitleReview) {
			review = &children[i]
			break
		}
	}
	if review == nil {
		t.Fatal("expected review subtask")
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete review: %v", err)
	}

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
