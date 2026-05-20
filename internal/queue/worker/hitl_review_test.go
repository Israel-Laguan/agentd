package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
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

func setupLegacyReviewProfile(t *testing.T, store models.KanbanStore, ctx context.Context, provider string) {
	t.Helper()
	profile, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profile.RequireReview = true
	profile.AgenticMode = false
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

func TestProcess_LegacyRequireReview_DraftAndFinalPayloadUseRawStdout(t *testing.T) {
	t.Parallel()
	store := &captureResultStore{FakeKanbanStore: testutil.NewFakeStore()}
	ctx := context.Background()
	setupLegacyReviewProfile(t, store, ctx, "")

	sbResult := sandbox.Result{Success: true, ExitCode: 0, Stdout: "clean output\n", Duration: 1500 * time.Millisecond}
	sb := &stdoutReviewSandbox{result: sbResult}
	w := NewWorker(store, &routingTestGateway{}, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	task := materializeLegacyReviewTask(t, store, ctx, "legacy-review-stdout", "task-review-stdout")
	queued, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("queue task: %v", err)
	}

	w.Process(ctx, *queued)
	assertLegacyReviewDraftRawStdout(t, store, ctx, task.ID, sbResult.Stdout)
	completeReviewSubtask(t, store, ctx, task.ID)
	requeued := requeueParentTask(t, store, ctx, task.ID)
	w.Process(ctx, requeued)
	assertLegacyReviewCommittedStdout(t, store, ctx, task.ID, sb, sbResult)
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

func assertLegacyReviewDraftRawStdout(t *testing.T, store models.KanbanStore, ctx context.Context, taskID, stdout string) {
	t.Helper()
	blocked, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get blocked parent: %v", err)
	}
	if blocked.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", blocked.State)
	}
	comments, err := store.ListComments(ctx, taskID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	draft, ok := findLatestDraftReview(comments)
	if !ok || strings.Contains(draft, "exit=") || !strings.Contains(draft, strings.TrimSpace(stdout)) {
		t.Fatalf("draft = %q, want raw stdout containing %q", draft, strings.TrimSpace(stdout))
	}
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

func assertLegacyReviewCommittedStdout(t *testing.T, store *captureResultStore, ctx context.Context, taskID string, sb *stdoutReviewSandbox, sbResult sandbox.Result) {
	t.Helper()
	final, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get final parent: %v", err)
	}
	if final.State != models.TaskStateCompleted {
		t.Fatalf("parent state = %s, want COMPLETED", final.State)
	}
	if store.lastResult == nil {
		t.Fatal("expected committed task result")
	}
	wantPayload := resultPayload(sandbox.Result{Success: true, Stdout: strings.TrimSpace(sbResult.Stdout)})
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
	setupLegacyReviewProfile(t, store, ctx, "")
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

func TestProcess_AgenticRequireReview_RejectionMarkedOnlyAfterDraft(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	setupAgenticReviewProfile(t, store, ctx)

	const rejectionComment = "Please add error handling"
	const revisedDraft = "revised draft with error handling"
	gw := &sequenceGateway{
		responses: []gateway.AIResponse{
			{
				Content: "I'll check the workspace first.",
				ToolCalls: []gateway.ToolCall{{
					ID: "call_reject_1", Type: "function",
					Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command": "pwd"}`},
				}},
			},
			{Content: revisedDraft},
		},
	}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"pwd": {Success: true, ExitCode: 0, Stdout: "/tmp\n"},
	}}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	task := materializeLegacyReviewTask(t, store, ctx, "agentic-review-reject", "task-agentic-review-reject")
	w.createReviewHandoff(ctx, task, "first draft output")
	failReviewWithComment(t, store, ctx, task.ID, rejectionComment)
	review := findReviewSubtask(t, store, ctx, task.ID)
	queued := requeueParentTask(t, store, ctx, task.ID)

	w.Process(ctx, queued)

	if gw.callCount != 2 {
		t.Fatalf("gateway calls = %d, want 2 (tool turn + final draft)", gw.callCount)
	}
	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if !isReviewRejectionConsumed(comments, review.ID) {
		t.Fatal("expected review-rejection-used marker after revised draft persisted")
	}
	draft, ok := findLatestDraftReview(comments)
	if !ok || !strings.Contains(draft, revisedDraft) {
		t.Fatalf("draft = %q, want revised draft containing %q", draft, revisedDraft)
	}
	parent, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", parent.State)
	}
}

func TestProcess_LegacyRequireReview_InjectsRejectionFeedback(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	setupLegacyReviewProfile(t, store, ctx, "ollama")
	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	task := materializeLegacyReviewTask(t, store, ctx, "legacy-review-reject", "task-review-reject")
	const rejectionComment = "Please add error handling"
	w.createReviewHandoff(ctx, task, "first draft output")
	failReviewWithComment(t, store, ctx, task.ID, rejectionComment)
	queued := requeueParentTask(t, store, ctx, task.ID)
	w.Process(ctx, queued)
	if len(gw.requests) != 1 {
		t.Fatalf("gateway requests = %d, want 1", len(gw.requests))
	}
	for _, msg := range gw.requests[0].Messages {
		if strings.Contains(msg.Content, rejectionComment) {
			if sb.execCount != 1 {
				t.Fatalf("sandbox executions = %d, want 1", sb.execCount)
			}
			return
		}
	}
	t.Fatalf("gateway messages missing rejection feedback %q", rejectionComment)
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
