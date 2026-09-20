package worker

import (
	"context"
	"os"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// setUpTieredDecisionFixture mirrors setUpTieredVerifyFixture but for the
// decision step, and additionally pre-spawns an execute task that already
// depends on (and, per the "already unlocked" scenario this simulates, is
// already READY on) the decision — the case handleNeedsContext's rewire
// must handle.
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
		{Task: tieredReadyExecuteDraft(origin.ProjectID), DependsOnID: "decision-task"},
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

func tieredReadyExecuteDraft(projectID string) models.Task {
	return models.Task{
		BaseEntity: models.BaseEntity{ID: "execute-task"},
		ProjectID:  projectID,
		AgentID:    "tier-execute",
		Title:      "execute: origin",
		// READY simulates the common race: UnlockReadyChildren already
		// promoted execute out of PENDING by the time the decision step's
		// own post-commit NEEDS_CONTEXT check runs.
		State:    models.TaskStateReady,
		Assignee: models.TaskAssigneeSystem,
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
	assertTieredExecuteRewiredAndBlocked(t, ctx, store, executeTask, newContextID, newDecisionID)
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

func assertTieredExecuteRewiredAndBlocked(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, executeTask models.Task, _, newDecisionID string) {
	t.Helper()
	// execute was READY, depending on the now-stale decision; it must be
	// rewired onto the new decision and blocked until that one is ready.
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
	if reloadedExecute.State != models.TaskStateBlocked {
		t.Fatalf("execute state = %s, want BLOCKED", reloadedExecute.State)
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
