package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

const breakdownMarkerTitle = "AGENT_PLAN"

func newBreakdownRollupFixture(t *testing.T) (*testutil.FakeKanbanStore, *acpReadOnlyLog, *Worker, models.Task) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(t.TempDir())
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "breakdown-rollup",
		Tasks:       []models.DraftTask{{Title: breakdownMarkerTitle, Description: script}},
	})
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = blockTaskWithBreakdown(ctx, store, *running, []models.DraftTask{
		{Title: script, SplitChild: true}, {Title: script, SplitChild: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	rt, _, log := acpRuntime(t)
	w := NewWorker(store, nil, &routingTestSandbox{}, nil, &mockEventSink{}, WorkerOptions{Routes: acpRoutes(), ACP: rt})
	return store, log, w, tasks[0]
}

func mustGetTask(t *testing.T, store *testutil.FakeKanbanStore, id string) models.Task {
	t.Helper()
	task, err := store.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTask(%s): %v", id, err)
	}
	return *task
}

func mustListChildren(t *testing.T, store *testutil.FakeKanbanStore, id string) []models.Task {
	t.Helper()
	children, err := store.ListChildTasks(context.Background(), id)
	if err != nil {
		t.Fatalf("ListChildTasks(%s): %v", id, err)
	}
	return children
}

// A decomposed parent that resumes after its subtasks complete must be rolled
// up to COMPLETED, not re-prompted: re-prompting the identical task made the
// model decompose it again on every resume.
func TestBreakdownRollup_ResumedParentCompletesWithoutRedecomposing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, log, w, parent := newBreakdownRollupFixture(t)

	if got := mustGetTask(t, store, parent.ID).State; got != models.TaskStateBlocked {
		t.Fatalf("parent state after breakdown = %s, want BLOCKED", got)
	}
	children := mustListChildren(t, store, parent.ID)
	if len(children) != 2 {
		t.Fatalf("children after breakdown = %d, want 2", len(children))
	}

	for _, child := range children {
		w.Process(ctx, mustGetTask(t, store, child.ID))
	}
	resumed := mustGetTask(t, store, parent.ID)
	if resumed.State != models.TaskStateReady {
		t.Fatalf("parent state after children completed = %s, want READY", resumed.State)
	}

	startsBefore, _ := log.runs()
	w.Process(ctx, resumed)

	if got := mustGetTask(t, store, parent.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("resumed parent state = %s, want COMPLETED", got)
	}
	if got := len(mustListChildren(t, store, parent.ID)); got != 2 {
		t.Fatalf("children after resume = %d, want 2 (parent was decomposed again)", got)
	}
	if startsAfter, _ := log.runs(); len(startsAfter) != len(startsBefore) {
		got := len(startsAfter) - len(startsBefore)
		t.Fatalf("resumed parent made %d gateway requests, want 0", got)
	}
}

// The batch dispatch path rolls a resumed parent up too, instead of sending
// it to the model alongside the other claimed tasks.
func TestBreakdownRollup_BatchPathCompletesResumedParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, log, w, parent := newBreakdownRollupFixture(t)

	children := mustListChildren(t, store, parent.ID)
	w.Process(ctx, mustGetTask(t, store, children[0].ID))
	w.Process(ctx, mustGetTask(t, store, children[1].ID))

	_, others, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "batch-partner",
		Tasks:       []models.DraftTask{{Title: fakeagent.Script(report(map[string]any{"summary": "done"})), AgentID: "default"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}

	startsBefore, _ := log.runs()
	w.ProcessBatch(ctx, []models.Task{mustGetTask(t, store, parent.ID), others[0]})

	if got := mustGetTask(t, store, others[0].ID).State; got != models.TaskStateCompleted {
		t.Fatalf("batch partner state = %s, want COMPLETED", got)
	}
	if got := mustGetTask(t, store, parent.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("resumed parent state = %s, want COMPLETED", got)
	}
	if got := len(mustListChildren(t, store, parent.ID)); got != 2 {
		t.Fatalf("children after batch resume = %d, want 2", got)
	}
	if startsAfter, _ := log.runs(); len(startsAfter)-len(startsBefore) != 1 {
		t.Fatalf("ACP sessions = %d, want only the batch partner", len(startsAfter)-len(startsBefore))
	}
}

// A parent resumed before its breakdown subtasks completed (e.g. a manual
// retry) is not rolled up and runs normally. On the agentic path its model
// reply cannot complete it while a subtask is still open (T-055b gap 2).
func TestBreakdownRollup_IncompleteSubtasksBlockAgenticCompletion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, log, w, parent := newBreakdownRollupFixture(t)

	blocked := mustGetTask(t, store, parent.ID)
	retried, err := store.UpdateTaskState(ctx, blocked.ID, blocked.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState(READY): %v", err)
	}

	startsBefore, _ := log.runs()
	w.Process(ctx, *retried)

	if startsAfter, _ := log.runs(); len(startsAfter)-len(startsBefore) != 1 {
		got := len(startsAfter) - len(startsBefore)
		t.Fatalf("gateway requests on retry = %d, want 1", got)
	}
	if got := mustGetTask(t, store, parent.ID).State; got == models.TaskStateCompleted {
		t.Fatal("model completed the parent while its subtasks were still open")
	}
}

func TestLatestBreakdownSubtaskCount_UsesNewestWorkerMarker(t *testing.T) {
	t.Parallel()
	t0 := time.Now()
	comments := []models.Comment{
		{BaseEntity: models.BaseEntity{CreatedAt: t0.Add(time.Second)}, Author: models.CommentAuthorWorkerAgent, Body: breakdownSubtasksPrefix + "3"},
		{BaseEntity: models.BaseEntity{CreatedAt: t0}, Author: models.CommentAuthorWorkerAgent, Body: breakdownSubtasksPrefix + "2"},
		{BaseEntity: models.BaseEntity{CreatedAt: t0.Add(2 * time.Second)}, Author: models.CommentAuthorWorkerAgent, Body: "unrelated"},
		// A user-authored lookalike is newer still and must be ignored.
		{BaseEntity: models.BaseEntity{CreatedAt: t0.Add(3 * time.Second)}, Author: models.CommentAuthorUser, Body: breakdownSubtasksPrefix + "99"},
	}
	count, ok := latestBreakdownSubtaskCount(comments)
	if !ok || count != 3 {
		t.Fatalf("latestBreakdownSubtaskCount = (%d, %v), want (3, true)", count, ok)
	}
	if _, ok := latestBreakdownSubtaskCount([]models.Comment{{Author: models.CommentAuthorWorkerAgent, Body: "plain"}}); ok {
		t.Fatal("non-marker comment was accepted as a breakdown marker")
	}
}

// A caller-supplied comment that claims fewer children than the parent
// actually has must not roll the parent up: the recorded child set is the
// relations table, and a subset claim would strand the other subtasks. On the
// agentic path the model's completion is refused for the same reason.
func TestBreakdownRollup_ForgedSubsetMarkerBlocksAgenticCompletion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, log, w, parent := newBreakdownRollupFixture(t)

	children := mustListChildren(t, store, parent.ID)
	w.Process(ctx, mustGetTask(t, store, children[0].ID))

	if err := store.AddComment(ctx, models.Comment{
		TaskID: parent.ID,
		Author: models.CommentAuthorUser,
		Body:   breakdownSubtasksPrefix + "1",
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	blocked := mustGetTask(t, store, parent.ID)
	retried, err := store.UpdateTaskState(ctx, blocked.ID, blocked.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState(READY): %v", err)
	}

	startsBefore, _ := log.runs()
	w.Process(ctx, *retried)

	if got := mustGetTask(t, store, parent.ID).State; got == models.TaskStateCompleted {
		t.Fatal("parent completed from a forged subset marker while a subtask was still open")
	}
	if startsAfter, _ := log.runs(); len(startsAfter)-len(startsBefore) != 1 {
		got := len(startsAfter) - len(startsBefore)
		t.Fatalf("gateway requests after forged marker = %d, want 1 (normal dispatch)", got)
	}
}

// A roll-up whose store reads fail must requeue the task instead of sending a
// parent whose subtasks already completed back to the model.
func TestBreakdownRollup_ReadFailureRequeuesInsteadOfDispatching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, log, w, parent := newBreakdownRollupFixture(t)

	for _, child := range mustListChildren(t, store, parent.ID) {
		w.Process(ctx, mustGetTask(t, store, child.ID))
	}

	w = NewWorker(&breakdownListCommentsFailureStore{FakeKanbanStore: store}, nil,
		&routingTestSandbox{}, nil, &mockEventSink{}, WorkerOptions{MaxToolIterations: 5})

	resumed := mustGetTask(t, store, parent.ID)
	startsBefore, _ := log.runs()
	w.Process(ctx, resumed)

	if startsAfter, _ := log.runs(); len(startsAfter) != len(startsBefore) {
		got := len(startsAfter) - len(startsBefore)
		t.Fatalf("gateway requests after a failed roll-up read = %d, want 0", got)
	}
	if got := mustGetTask(t, store, parent.ID).State; got != models.TaskStateReady {
		t.Fatalf("parent state after failed roll-up read = %s, want READY (requeued)", got)
	}
}

// breakdownListCommentsFailureStore fails the roll-up's first store read, the
// way a transient database error does.
type breakdownListCommentsFailureStore struct {
	*testutil.FakeKanbanStore
}

func (s *breakdownListCommentsFailureStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return nil, errors.New("comments table unavailable")
}

// breakdownCommitFailureStore fails the parent's result commit, the way a
// transient write error does.
type breakdownCommitFailureStore struct {
	*testutil.FakeKanbanStore
}

func (s *breakdownCommitFailureStore) UpdateTaskResult(ctx context.Context, id string, at time.Time, result models.TaskResult) (*models.Task, error) {
	if result.Success {
		return nil, errors.New("tasks table read-only")
	}
	return s.FakeKanbanStore.UpdateTaskResult(ctx, id, at, result)
}

// A roll-up whose commit fails must not report the task handled: the parent is
// requeued so the roll-up is retried instead of being left to reconciliation.
func TestBreakdownRollup_CommitFailureRequeuesParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, w, parent := newBreakdownRollupFixture(t)

	for _, child := range mustListChildren(t, store, parent.ID) {
		w.Process(ctx, mustGetTask(t, store, child.ID))
	}

	failing := &breakdownCommitFailureStore{FakeKanbanStore: store}
	handled, err := NewWorker(failing, nil, &routingTestSandbox{}, nil, &mockEventSink{},
		WorkerOptions{MaxToolIterations: 5}).tryRollUpBreakdown(ctx, mustGetTask(t, store, parent.ID))
	if !handled || err == nil {
		t.Fatalf("tryRollUpBreakdown = (%v, %v), want (true, error) so the caller requeues", handled, err)
	}
	if got := mustGetTask(t, store, parent.ID).State; got == models.TaskStateCompleted {
		t.Fatalf("parent state = COMPLETED despite a failed roll-up commit")
	}
}
