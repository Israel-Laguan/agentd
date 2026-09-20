package worker

import (
	"context"
	"os"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// setUpTieredDecisionFixture mirrors setUpTieredVerifyFixture but for the
// decision step, and additionally pre-spawns an execute task that depends
// on the decision. The execute stays PENDING until the decision completes,
// so the NEEDS_CONTEXT rewire keeps it PENDING (the PENDING-dependent
// reroute). This exercises the PENDING branch of RewireDependsOn; the
// READY->BLOCKED demotion is exercised separately via
// TestRewireDependsOn_RedirectsPendingAndBlocksReady.
func setUpTieredDecisionFixture(t *testing.T, store *testutil.FakeKanbanStore) (origin, decisionTask, executeTask models.Task, workspace string) {
	t.Helper()
	origin, workspace = setUpTieredOriginRunning(t, store, "tiered-decision-fixture")

	pack := &ContextPack{
		Version:      ContextPackVersion,
		TaskID:       "context-1",
		ParentTaskID: origin.ID,
		Summary:      "test summary",
		Paths:        []string{"a.go"},
	}
	writeTieredDecisionPack(t, workspace, pack)

	ctx := context.Background()
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: "tier-decision", Provider: "test-provider", Model: "test-model", AgenticMode: true,
	}); err != nil {
		t.Fatalf("upsert tier-decision profile: %v", err)
	}

	created, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: tieredDecisionDraft(origin.ProjectID)},
		{Task: tieredPendingExecuteDraft(origin.ProjectID), DependsOnID: "decision-task"},
	})
	if err != nil {
		t.Fatalf("spawn decision/execute steps: %v", err)
	}
	return origin, created[0], created[1], workspace
}

func setUpTieredOriginRunning(t *testing.T, store *testutil.FakeKanbanStore, projectName string) (models.Task, string) {
	t.Helper()
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: projectName,
		Tasks:       []models.DraftTask{{Title: "origin", Description: "complex task"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	originTask, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark origin running: %v", err)
	}
	originTask, err = store.UpdateTaskState(ctx, originTask.ID, originTask.UpdatedAt, models.TaskStateBlocked)
	if err != nil {
		t.Fatalf("block origin: %v", err)
	}

	project, err := store.GetProject(ctx, originTask.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	workspace := project.WorkspacePath
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })
	return *originTask, workspace
}

func writeTieredDecisionPack(t *testing.T, workspace string, pack *ContextPack) {
	t.Helper()
	if err := WriteContextPack(workspace, pack); err != nil {
		t.Fatalf("write context pack: %v", err)
	}
}

func tieredDecisionDraft(projectID string) models.Task {
	return models.Task{
		BaseEntity:  models.BaseEntity{ID: "decision-task"},
		ProjectID:   projectID,
		AgentID:     "tier-decision",
		Title:       "decision: origin",
		Description: "decide what to touch",
		State:       models.TaskStateReady,
		Assignee:    models.TaskAssigneeSystem,
	}
}

func tieredPendingExecuteDraft(projectID string) models.Task {
	return models.Task{
		BaseEntity: models.BaseEntity{ID: "execute-task"},
		ProjectID:  projectID,
		AgentID:    "tier-execute",
		Title:      "execute: origin",
		State:      models.TaskStatePending,
		Assignee:   models.TaskAssigneeSystem,
	}
}

func TestTieredDecision_NeedsContextSpawnsRegatherAndRewiresExecute(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin, decisionTask, executeTask, _ := setUpTieredDecisionFixture(t, store)

	gw := &plainTextVerifyGateway{content: `{"needs_context": true, "reason": "referenced file does not exist"}`}
	sb := &mockAgenticSandbox{}
	w := NewWorker(store, gw, sb, nil, &mockEventSink{}, WorkerOptions{MaxToolIterations: 10})

	w.Process(ctx, decisionTask)
	assertTieredDecisionReachedNeedsContext(t, ctx, store, decisionTask)
	newContextID, newDecisionID := assertTieredRegatherPairSpawned(t, ctx, store, origin)
	assertTieredPendingExecuteRewiredStillPending(t, ctx, store, executeTask, newContextID, newDecisionID)
}

func assertTieredDecisionReachedNeedsContext(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, decisionTask models.Task) {
	t.Helper()
	current, err := store.GetTask(ctx, decisionTask.ID)
	if err != nil {
		t.Fatalf("GetTask(decision): %v", err)
	}
	if current.State != models.TaskStateNeedsContext {
		t.Fatalf("decision state = %s, want NEEDS_CONTEXT", current.State)
	}
}

func assertTieredRegatherPairSpawned(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, origin models.Task) (newContextID, newDecisionID string) {
	t.Helper()
	spawned, err := store.ListChildTasksByRelation(ctx, origin.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation: %v", err)
	}
	for _, s := range spawned {
		if strings.HasPrefix(s.Title, "context (re-gather") {
			newContextID = s.ID
		}
		if strings.HasPrefix(s.Title, "decision (re-gather") {
			newDecisionID = s.ID
		}
	}
	if newContextID == "" || newDecisionID == "" {
		t.Fatalf("expected a re-gather context+decision pair among spawned children, got %+v", spawned)
	}

	newDecisionParents, err := store.ListParentTasksByRelation(ctx, newDecisionID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(newDecision): %v", err)
	}
	if len(newDecisionParents) != 1 || newDecisionParents[0].ID != newContextID {
		t.Fatalf("new decision's DEPENDS_ON parents = %+v, want [%s]", newDecisionParents, newContextID)
	}
	return newContextID, newDecisionID
}

func assertTieredPendingExecuteRewiredStillPending(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, executeTask models.Task, _, newDecisionID string) {
	t.Helper()
	// execute is PENDING and depends on the now-stale decision; after
	// NEEDS_CONTEXT it must be rewired onto the new decision without
	// changing state (PENDING stays PENDING; only READY is demoted to
	// BLOCKED by RewireDependsOn).
	executeParents, err := store.ListParentTasksByRelation(ctx, executeTask.ID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(execute): %v", err)
	}
	if len(executeParents) != 1 || executeParents[0].ID != newDecisionID {
		t.Fatalf("execute's DEPENDS_ON parents = %+v, want [%s]", executeParents, newDecisionID)
	}
	reloadedExecute, err := store.GetTask(ctx, executeTask.ID)
	if err != nil {
		t.Fatalf("GetTask(execute): %v", err)
	}
	if reloadedExecute.State != models.TaskStatePending {
		t.Fatalf("execute state = %s, want PENDING", reloadedExecute.State)
	}
}

func TestReconcileBlockedDependents_ReReadiesOnceParentCompletes(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin := newTieredPipelineFixture(t, store)
	w := &Worker{store: store, sink: &mockEventSink{}}

	newDecision, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "new-decision"}, ProjectID: origin.ProjectID, AgentID: "tier-decision", Title: "decision (re-gather v2)", State: models.TaskStateReady, Assignee: models.TaskAssigneeSystem}},
	})
	if err != nil {
		t.Fatalf("spawn new decision: %v", err)
	}
	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "execute-blocked"}, ProjectID: origin.ProjectID, AgentID: "tier-execute", Title: "execute: origin", State: models.TaskStateBlocked, Assignee: models.TaskAssigneeSystem}, DependsOnID: newDecision[0].ID},
	}); err != nil {
		t.Fatalf("spawn blocked execute: %v", err)
	}

	// Not yet resolved: newDecision is still READY, not COMPLETED.
	w.reconcileBlockedDependents(ctx, newDecision[0].ID)
	stillBlocked, err := store.GetTask(ctx, "execute-blocked")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stillBlocked.State != models.TaskStateBlocked {
		t.Fatalf("execute state = %s, want still BLOCKED before decision completes", stillBlocked.State)
	}

	if _, err := store.UpdateTaskState(ctx, newDecision[0].ID, newDecision[0].UpdatedAt, models.TaskStateRunning); err != nil {
		t.Fatalf("transition new decision to RUNNING: %v", err)
	}
	reloadedDecision, err := store.GetTask(ctx, newDecision[0].ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := store.UpdateTaskResult(ctx, reloadedDecision.ID, reloadedDecision.UpdatedAt, models.TaskResult{Success: true, Payload: "decision output"}); err != nil {
		t.Fatalf("complete new decision: %v", err)
	}

	w.reconcileBlockedDependents(ctx, newDecision[0].ID)
	reReadied, err := store.GetTask(ctx, "execute-blocked")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if reReadied.State != models.TaskStateReady {
		t.Fatalf("execute state = %s, want READY after decision completes", reReadied.State)
	}
}

func TestTieredDecision_PendingReGatherNeedsContextSpawnsSecondRegather(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin, _, _, _ := setUpTieredDecisionFixture(t, store)

	// The re-gather decision spawned by handleNeedsContext starts as PENDING.
	// Simulate that second-generation PENDING decision itself signaling
	// NEEDS_CONTEXT (insufficient pack even after re-gather).
	w := &Worker{store: store, sink: &mockEventSink{}, tieredCfg: config.TieredConfig{
		Escalation: config.TieredEscalationConfig{MaxReGather: 5},
	}}
	// Trigger first re-gather to create a PENDING newDecision.
	firstDecision, err := store.GetTask(ctx, "decision-task")
	if err != nil {
		t.Fatalf("GetTask(decision): %v", err)
	}
	if err := w.handleNeedsContext(ctx, *firstDecision, origin, "first pack missing file"); err != nil {
		t.Fatalf("handleNeedsContext first: %v", err)
	}
	spawned, err := store.ListChildTasksByRelation(ctx, origin.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation: %v", err)
	}
	var pendingRegatherDecision models.Task
	for _, s := range spawned {
		if strings.HasPrefix(s.Title, "decision (re-gather") {
			pendingRegatherDecision = s
		}
	}
	if pendingRegatherDecision.ID == "" {
		t.Fatalf("no re-gather decision found among %+v", spawned)
	}
	if pendingRegatherDecision.State != models.TaskStatePending {
		t.Fatalf("re-gather decision state = %s, want PENDING", pendingRegatherDecision.State)
	}
	// Move PENDING -> RUNNING so the state transition to NEEDS_CONTEXT is
	// realistic (PENDING->NEEDS_CONTEXT is also valid, but RUNNING mirrors
	// the production flow where decision was dispatched).
	running, err := store.UpdateTaskState(ctx, pendingRegatherDecision.ID, pendingRegatherDecision.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("RUNNING: %v", err)
	}
	pendingRegatherDecision = *running
	originReloaded, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask(origin): %v", err)
	}
	if err := w.handleNeedsContext(ctx, pendingRegatherDecision, *originReloaded, "still missing file"); err != nil {
		t.Fatalf("handleNeedsContext second (PENDING origin): %v", err)
	}
	secondDecision, err := store.GetTask(ctx, pendingRegatherDecision.ID)
	if err != nil {
		t.Fatalf("GetTask(second decision): %v", err)
	}
	if secondDecision.State != models.TaskStateNeedsContext {
		t.Fatalf("second re-gather decision state = %s, want NEEDS_CONTEXT", secondDecision.State)
	}
	spawned2, err := store.ListChildTasksByRelation(ctx, origin.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation after second: %v", err)
	}
	// Should have 2 generations of context+decision pairs plus originals.
	hasSecondContext := false
	for _, s := range spawned2 {
		if s.Title == "context (re-gather v2): origin" || strings.HasPrefix(s.Title, "context (re-gather v2)") {
			hasSecondContext = true
		}
	}
	if !hasSecondContext {
		t.Fatalf("expected second re-gather context among %+v", spawned2)
	}
}

func TestTieredDecision_NeedsContextCapHandsOffToHuman(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin, decisionTask, _, _ := setUpTieredDecisionFixture(t, store)
	w := &Worker{store: store, sink: &mockEventSink{}, tieredCfg: config.TieredConfig{
		Escalation: config.TieredEscalationConfig{MaxReGather: 1},
	}}

	// Exhaust the single re-gather budget by pre-creating a context child
	// so nextContextPackGeneration will be 2 > MaxReGather(1).
	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "pre-context"}, ProjectID: origin.ProjectID, AgentID: "tier-context", Title: "context (re-gather v1): origin", State: models.TaskStateCompleted, Assignee: models.TaskAssigneeSystem}},
	}); err != nil {
		t.Fatalf("pre-create context: %v", err)
	}
	// decisionTask is READY; move to RUNNING before handleNeedsContext
	// mirrors production (or handleNeedsContext will reload and transition).
	running, err := store.UpdateTaskState(ctx, decisionTask.ID, decisionTask.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("RUNNING: %v", err)
	}
	originReloaded, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask(origin): %v", err)
	}
	if err := w.handleNeedsContext(ctx, *running, *originReloaded, "cap test"); err != nil {
		t.Fatalf("handleNeedsContext: %v", err)
	}
	gotOrigin, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask(origin after cap): %v", err)
	}
	if gotOrigin.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("origin state = %s, want FAILED_REQUIRES_HUMAN after MaxReGather cap", gotOrigin.State)
	}
	// Decision itself should still have been moved to NEEDS_CONTEXT before cap check.
	gotDecision, err := store.GetTask(ctx, decisionTask.ID)
	if err != nil {
		t.Fatalf("GetTask(decision): %v", err)
	}
	if gotDecision.State != models.TaskStateNeedsContext {
		t.Fatalf("decision state = %s, want NEEDS_CONTEXT", gotDecision.State)
	}
}

func TestTieredNeedsContext_RevivalTransitions(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin := newTieredPipelineFixture(t, store)
	// Create a decision in NEEDS_CONTEXT then revive it to READY (the
	// "revival" path once a fresh pack exists).
	needsCtxTask := models.Task{BaseEntity: models.BaseEntity{ID: "needs-ctx"}, ProjectID: origin.ProjectID, AgentID: "tier-decision", Title: "decision: origin", State: models.TaskStatePending, Assignee: models.TaskAssigneeSystem}
	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{{Task: needsCtxTask}}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pending, err := store.GetTask(ctx, "needs-ctx")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	ctxTask, err := store.UpdateTaskState(ctx, pending.ID, pending.UpdatedAt, models.TaskStateNeedsContext)
	if err != nil {
		t.Fatalf("to NEEDS_CONTEXT: %v", err)
	}
	if !ctxTask.State.CanTransitionTo(models.TaskStateReady) {
		t.Fatalf("NEEDS_CONTEXT->READY should be valid")
	}
	revived, err := store.UpdateTaskState(ctx, ctxTask.ID, ctxTask.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("revive to READY: %v", err)
	}
	if revived.State != models.TaskStateReady {
		t.Fatalf("revived state = %s, want READY", revived.State)
	}
	// NEEDS_CONTEXT -> FAILED_REQUIRES_HUMAN also valid (abandon re-gather).
	needs2 := models.Task{BaseEntity: models.BaseEntity{ID: "needs-ctx-2"}, ProjectID: origin.ProjectID, AgentID: "tier-decision", Title: "decision 2", State: models.TaskStatePending, Assignee: models.TaskAssigneeSystem}
	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{{Task: needs2}}); err != nil {
		t.Fatalf("spawn2: %v", err)
	}
	p2, err := store.GetTask(ctx, "needs-ctx-2")
	if err != nil {
		t.Fatalf("GetTask2: %v", err)
	}
	ctx2, err := store.UpdateTaskState(ctx, p2.ID, p2.UpdatedAt, models.TaskStateNeedsContext)
	if err != nil {
		t.Fatalf("to NEEDS_CONTEXT 2: %v", err)
	}
	failed, err := store.UpdateTaskState(ctx, ctx2.ID, ctx2.UpdatedAt, models.TaskStateFailedRequiresHuman)
	if err != nil {
		t.Fatalf("to FAILED_REQUIRES_HUMAN: %v", err)
	}
	if failed.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state = %s, want FAILED_REQUIRES_HUMAN", failed.State)
	}
}

func TestDispatchTieredStep_StaleDepWithCompletedFreshReReadies(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	origin, staleDecision, executeTask, _ := setUpTieredDecisionFixture(t, store)
	// Move stale decision to NEEDS_CONTEXT.
	staleRunning, err := store.UpdateTaskState(ctx, staleDecision.ID, staleDecision.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("stale RUNNING: %v", err)
	}
	staleNeedsCtx, err := store.UpdateTaskState(ctx, staleRunning.ID, staleRunning.UpdatedAt, models.TaskStateNeedsContext)
	if err != nil {
		t.Fatalf("stale NEEDS_CONTEXT: %v", err)
	}
	// Spawn a fresh decision that is already COMPLETED (simulates dispatch
	// racing after the re-gather finished). Ensure its CreatedAt is after
	// stale's so freshDecisionID (which compares CreatedAt) picks it up.
	freshTime := staleNeedsCtx.CreatedAt.Add(10 * 1e9)
	freshPending := models.Task{
		BaseEntity: models.BaseEntity{ID: "fresh-decision-completed", CreatedAt: freshTime, UpdatedAt: freshTime},
		ProjectID: origin.ProjectID, AgentID: "tier-decision", Title: "decision (re-gather v2): origin", State: models.TaskStatePending, Assignee: models.TaskAssigneeSystem,
	}
	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{{Task: freshPending, DependsOnID: ""}}); err != nil {
		t.Fatalf("spawn fresh: %v", err)
	}
	running, err := store.UpdateTaskState(ctx, freshPending.ID, freshTime, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("fresh RUNNING: %v", err)
	}
	completed, err := store.UpdateTaskResult(ctx, running.ID, running.UpdatedAt, models.TaskResult{Success: true, Payload: "decision output"})
	if err != nil {
		t.Fatalf("fresh COMPLETED: %v", err)
	}
	_ = completed
	// Dependent execute is PENDING on stale decision; dispatching it now
	// should detect stale NEEDS_CONTEXT, find the completed fresh, rewire
	// to it, and promote to READY instead of leaving it BLOCKED forever.
	project, err := store.GetProject(ctx, origin.ProjectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	w := &Worker{store: store, sink: &mockEventSink{}}
	execLatest, err := store.GetTask(ctx, executeTask.ID)
	if err != nil {
		t.Fatalf("GetTask execute: %v", err)
	}
	profile := models.AgentProfile{ID: "tier-execute", Provider: "test-provider", AgenticMode: true}
	// Ensure profile exists for dispatch lookup, though dispatch doesn't need gateway.
	if err := store.UpsertAgentProfile(ctx, profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	handled := w.dispatchTieredStep(ctx, *execLatest, *project, profile)
	if !handled {
		t.Fatalf("dispatchTieredStep returned false, want true (handled stale dep)")
	}
	rewiredParents, err := store.ListParentTasksByRelation(ctx, executeTask.ID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParent: %v", err)
	}
	if len(rewiredParents) != 1 || rewiredParents[0].ID != "fresh-decision-completed" {
		t.Fatalf("execute parents = %+v, want [fresh-decision-completed]", rewiredParents)
	}
	gotExec, err := store.GetTask(ctx, executeTask.ID)
	if err != nil {
		t.Fatalf("GetTask exec after: %v", err)
	}
	if gotExec.State != models.TaskStateReady {
		t.Fatalf("execute state = %s, want READY after rewiring to completed fresh", gotExec.State)
	}
}
