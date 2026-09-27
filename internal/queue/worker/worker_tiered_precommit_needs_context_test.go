package worker

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// spawnTieredExecuteStep spawns a READY execute step under origin, mirroring
// spawnTieredVerifyStep for the verify step.
func spawnTieredExecuteStep(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, origin models.Task) models.Task {
	t.Helper()
	executeDraft := models.Task{
		BaseEntity:  models.BaseEntity{ID: "execute-task"},
		ProjectID:   origin.ProjectID,
		AgentID:     "tier-execute",
		Title:       "execute: origin",
		Description: "make the change",
		State:       models.TaskStateReady,
		Assignee:    models.TaskAssigneeSystem,
	}
	created, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{{Task: executeDraft}})
	if err != nil {
		t.Fatalf("spawn execute step: %v", err)
	}
	return created[0]
}

func upsertTieredExecuteProfile(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore) {
	t.Helper()
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID:          "tier-execute",
		Provider:    "test-provider",
		Model:       "test-model",
		AgenticMode: true,
	}); err != nil {
		t.Fatalf("upsert tier-execute profile: %v", err)
	}
}

// spawnBlockedDependent spawns a BLOCKED task depending on parentStepID, used
// to prove that a pre-commit NEEDS_CONTEXT diversion never unlocks
// dependents (the T-023 race this interception exists to close).
func spawnBlockedDependent(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, origin models.Task, parentStepID string) models.Task {
	t.Helper()
	dependentDraft := models.Task{
		BaseEntity: models.BaseEntity{ID: "downstream-task"},
		ProjectID:  origin.ProjectID,
		AgentID:    escalateAgentID,
		Title:      "escalate: origin",
		State:      models.TaskStateBlocked,
		Assignee:   models.TaskAssigneeSystem,
	}
	created, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: dependentDraft, DependsOnID: parentStepID},
	})
	if err != nil {
		t.Fatalf("spawn blocked dependent: %v", err)
	}
	return created[0]
}

func assertTieredStepReachedNeedsContext(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, stepID string) {
	t.Helper()
	current, err := store.GetTask(ctx, stepID)
	if err != nil {
		t.Fatalf("GetTask(%s): %v", stepID, err)
	}
	if current.State != models.TaskStateNeedsContext {
		t.Fatalf("state = %s, want NEEDS_CONTEXT", current.State)
	}
}

func assertStillBlocked(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, taskID string) {
	t.Helper()
	current, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask(%s): %v", taskID, err)
	}
	if current.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want still BLOCKED (pre-commit interception must never unlock dependents)", current.State)
	}
}

func assertNoResultEventRecorded(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, taskID string) {
	t.Helper()
	events, err := store.ListEventsByTask(ctx, taskID)
	if err != nil {
		t.Fatalf("ListEventsByTask(%s): %v", taskID, err)
	}
	for _, e := range events {
		if e.Type == models.EventTypeResult {
			t.Fatalf("task %s has a RESULT event %+v; pre-commit interception must never reach the normal commit path", taskID, e)
		}
	}
}

// assertTieredDependentRewiredTo checks the BLOCKED dependent now hangs off
// the fresh decision instead of the stale step, then completes that decision
// and reconciles so the dependent becomes READY. Asserting only that the
// dependent stayed BLOCKED would not catch a regression leaving it attached
// to the step that can never complete.
func assertTieredDependentRewiredTo(t *testing.T, ctx context.Context, w *Worker, store *testutil.FakeKanbanStore, dependent, staleStep models.Task, newDecisionID string) {
	t.Helper()
	parents, err := store.ListParentTasksByRelation(ctx, dependent.ID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(dependent): %v", err)
	}
	if len(parents) != 1 || parents[0].ID != newDecisionID {
		t.Fatalf("dependent DEPENDS_ON parents = %+v, want only the fresh decision %s", parents, newDecisionID)
	}
	if parents[0].ID == staleStep.ID {
		t.Fatalf("dependent still attached to the stale step %s", staleStep.ID)
	}
	decision, err := store.GetTask(ctx, newDecisionID)
	if err != nil {
		t.Fatalf("GetTask(newDecision): %v", err)
	}
	if _, err := store.UpdateTaskResult(ctx, decision.ID, decision.UpdatedAt, models.TaskResult{Success: true, Payload: "fresh decision"}); err != nil {
		t.Fatalf("complete fresh decision: %v", err)
	}
	w.reconcileBlockedDependents(ctx, newDecisionID)
	ready, err := store.GetTask(ctx, dependent.ID)
	if err != nil {
		t.Fatalf("GetTask(dependent): %v", err)
	}
	if ready.State != models.TaskStateReady {
		t.Fatalf("dependent state after the fresh decision completed = %s, want READY", ready.State)
	}
}

func TestTieredVerify_NeedsContextInterceptsBeforeCommit(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin, verify, _ := setUpTieredVerifyFixture(t, store)
	dependent := spawnBlockedDependent(t, ctx, store, origin, verify.ID)

	gw := &plainTextVerifyGateway{content: `{"needs_context": true, "reason": "referenced file does not exist"}`}
	sb := &mockAgenticSandbox{}
	w := NewWorker(store, gw, sb, nil, &mockEventSink{}, WorkerOptions{MaxToolIterations: 10})

	w.Process(ctx, verify)

	assertTieredStepReachedNeedsContext(t, ctx, store, verify.ID)
	assertStillBlocked(t, ctx, store, dependent.ID)
	assertNoResultEventRecorded(t, ctx, store, verify.ID)
	_, newDecisionID := assertTieredRegatherPairSpawned(t, ctx, store, origin)
	assertTieredDependentRewiredTo(t, ctx, w, store, dependent, verify, newDecisionID)

	events, err := store.ListEventsByTask(ctx, verify.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask(verify): %v", err)
	}
	for _, e := range events {
		if string(e.Type) == tieredVerifyOutcomeEvent {
			t.Fatalf("verify task has a %s event %+v; the normal pass/fail/escalate classification must be skipped once pre-commit intercepted", tieredVerifyOutcomeEvent, e)
		}
	}
}

func TestTieredExecute_NeedsContextInterceptsBeforeCommit(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin, workspace := setUpTieredVerifyOrigin(t, store)
	writeTieredVerifyPack(t, workspace, origin.ID)
	upsertTieredExecuteProfile(t, ctx, store)
	execute := spawnTieredExecuteStep(t, ctx, store, origin)
	dependent := spawnBlockedDependent(t, ctx, store, origin, execute.ID)

	gw := &plainTextVerifyGateway{content: `{"needs_context": true, "reason": "touch_list references a file that was deleted upstream"}`}
	sb := &mockAgenticSandbox{}
	w := NewWorker(store, gw, sb, nil, &mockEventSink{}, WorkerOptions{MaxToolIterations: 10})

	w.Process(ctx, execute)

	assertTieredStepReachedNeedsContext(t, ctx, store, execute.ID)
	assertStillBlocked(t, ctx, store, dependent.ID)
	assertNoResultEventRecorded(t, ctx, store, execute.ID)
	_, newDecisionID := assertTieredRegatherPairSpawned(t, ctx, store, origin)
	assertTieredDependentRewiredTo(t, ctx, w, store, dependent, execute, newDecisionID)
}

func TestParseNeedsContextSignal_RequiresFlagAndReason(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "plain JSON", input: `{"needs_context": true, "reason": "missing file"}`, want: true},
		{name: "markdown fence", input: "```json\n{\"needs_context\": true, \"reason\": \"missing file\"}\n```", want: true},
		{name: "surrounding prose", input: "Here is my answer:\n```json\n{\"needs_context\": true, \"reason\": \"missing file\"}\n```\nThanks.", want: true},
		{name: "flag false", input: `{"needs_context": false, "reason": "missing file"}`, want: false},
		{name: "missing reason", input: `{"needs_context": true, "reason": ""}`, want: false},
		{name: "no needs_context field at all", input: `{"results":[{"check":"go test","outcome":"pass"}],"overall":"pass"}`, want: false},
		{name: "malformed JSON", input: `{not-json`, want: false},
		{name: "plain text, no JSON", input: "the change is complete", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, ok := parseNeedsContextSignal(test.input)
			if ok != test.want {
				t.Fatalf("parseNeedsContextSignal(%q) ok = %t, want %t", test.input, ok, test.want)
			}
		})
	}
}

func TestTieredVerify_NormalPassStillCommitsNormally(t *testing.T) {
	// Regression guard: a legitimate VerifyResult must never be misread as
	// a needs-context signal by the new pre-commit interception.
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, verify, _ := setUpTieredVerifyFixture(t, store)

	gw := &plainTextVerifyGateway{content: `{"results":[{"check":"go test ./...","outcome":"pass","detail":""}],"overall":"pass"}`}
	sb := &mockAgenticSandbox{}
	w := NewWorker(store, gw, sb, nil, &storeEventSink{store: store}, WorkerOptions{MaxToolIterations: 10})

	w.Process(ctx, verify)

	current, err := store.GetTask(ctx, verify.ID)
	if err != nil {
		t.Fatalf("GetTask(verify): %v", err)
	}
	if current.State != models.TaskStateCompleted {
		t.Fatalf("verify state = %s, want COMPLETED (normal pass path must be unaffected)", current.State)
	}
}

// regatherSpawnFailureStore fails only the re-gather chain spawn, leaving the
// rest of the store usable so a test can observe the post-transition failure.
type regatherSpawnFailureStore struct {
	*testutil.FakeKanbanStore
}

func (s *regatherSpawnFailureStore) SpawnTieredContinuation(context.Context, string, []models.TieredContinuationTask) ([]models.Task, error) {
	return nil, errors.New("spawn unavailable")
}

// A re-gather that cannot be scheduled must not leave the step parked in
// NEEDS_CONTEXT with no committed result: it is abandoned so the dependent
// parked behind it fails explicitly instead of blocking forever.
func TestTieredExecute_FailedRegatherAbandonsTheStep(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin, workspace := setUpTieredVerifyOrigin(t, store)
	writeTieredVerifyPack(t, workspace, origin.ID)
	upsertTieredExecuteProfile(t, ctx, store)
	execute := spawnTieredExecuteStep(t, ctx, store, origin)
	dependent := spawnBlockedDependent(t, ctx, store, origin, execute.ID)

	gw := &plainTextVerifyGateway{content: `{"needs_context": true, "reason": "the referenced file is gone"}`}
	w := NewWorker(&regatherSpawnFailureStore{FakeKanbanStore: store}, gw, &mockAgenticSandbox{},
		nil, &mockEventSink{}, WorkerOptions{MaxToolIterations: 10})

	w.Process(ctx, execute)

	step, err := store.GetTask(ctx, execute.ID)
	if err != nil {
		t.Fatalf("GetTask(execute): %v", err)
	}
	if step.State != models.TaskStateFailed {
		t.Fatalf("execute state after a failed re-gather = %s, want FAILED (not parked in NEEDS_CONTEXT)", step.State)
	}
	dep, err := store.GetTask(ctx, dependent.ID)
	if err != nil {
		t.Fatalf("GetTask(dependent): %v", err)
	}
	if dep.State != models.TaskStateFailed {
		t.Fatalf("dependent state = %s, want FAILED (its predecessor can never run)", dep.State)
	}
}

// diversionCheckFailureStore fails every GetTask call so confirmNotDiverted
// exhausts its retries and records TIERED_VERIFY_DIVERSION_CHECK_ERROR.
type diversionCheckFailureStore struct {
	*testutil.FakeKanbanStore
}

func (s *diversionCheckFailureStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	return nil, errors.New("tasks table unavailable")
}

// A failed diversion check leaves the verify outcome unknown, so the step must
// be failed explicitly rather than classified as a parse error / verify
// failure for output that was never a verify result.
func TestTieredVerify_DiversionCheckFailureDoesNotClassifyOutput(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, verify, _ := setUpTieredVerifyFixture(t, store)

	gw := &plainTextVerifyGateway{content: `{"results":[{"check":"go test","outcome":"pass"}],"overall":"pass"}`}
	failing := &diversionCheckFailureStore{FakeKanbanStore: store}
	w := NewWorker(failing, gw, &mockAgenticSandbox{}, nil, &storeEventSink{store: store}, WorkerOptions{MaxToolIterations: 10})

	w.Process(ctx, verify)

	events, err := store.ListEventsByTask(ctx, verify.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask(verify): %v", err)
	}
	var foundDiversionError bool
	for _, e := range events {
		if string(e.Type) == tieredVerifyOutcomeEvent {
			t.Fatalf("verify task has a %s event %+v; output must not be classified when the diversion state is unknown", tieredVerifyOutcomeEvent, e)
		}
		if string(e.Type) == "TIERED_VERIFY_DIVERSION_CHECK_ERROR" {
			foundDiversionError = true
		}
	}
	if !foundDiversionError {
		t.Fatal("expected TIERED_VERIFY_DIVERSION_CHECK_ERROR event when diversion check fails")
	}
}
