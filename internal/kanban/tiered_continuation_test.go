package kanban

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

func newTieredOriginTask(t *testing.T, store *Store, ctx context.Context) models.Task {
	t.Helper()
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "tiered-continuation-fixture",
		Tasks:       []models.DraftTask{{Title: "origin", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	_ = project
	origin, err := store.UpdateTaskState(ctx, tasks[0].ID, tasks[0].UpdatedAt, models.TaskStateBlocked)
	if err != nil {
		t.Fatalf("UpdateTaskState(BLOCKED) error = %v", err)
	}
	return *origin
}

func TestSpawnTieredContinuation_WiresSpawnedByAndDependsOn(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	origin := newTieredOriginTask(t, store, ctx)

	now := time.Now().UTC()
	first := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   origin.ProjectID,
		AgentID:     "tier-context",
		Title:       "context (re-gather v2)",
		Description: "d",
		State:       models.TaskStateReady,
		Assignee:    models.TaskAssigneeSystem,
	}
	second := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   origin.ProjectID,
		AgentID:     "tier-decision",
		Title:       "decision (re-gather v2)",
		Description: "d",
		State:       models.TaskStatePending,
		Assignee:    models.TaskAssigneeSystem,
	}

	created, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: first},
		{Task: second, DependsOnID: first.ID},
	})
	if err != nil {
		t.Fatalf("SpawnTieredContinuation() error = %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("len(created) = %d, want 2", len(created))
	}

	// Origin's own state must be untouched (unlike PersistTieredDAG, which
	// blocks the parent — the origin is already BLOCKED by that point).
	reloadedOrigin, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask(origin) error = %v", err)
	}
	if reloadedOrigin.State != models.TaskStateBlocked {
		t.Fatalf("origin state = %s, want unchanged BLOCKED", reloadedOrigin.State)
	}

	spawned, err := store.ListChildTasksByRelation(ctx, origin.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation(SPAWNED_BY) error = %v", err)
	}
	if len(spawned) != 2 {
		t.Fatalf("len(spawned) = %d, want 2", len(spawned))
	}

	dependsOnParents, err := store.ListParentTasksByRelation(ctx, second.ID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(DEPENDS_ON) error = %v", err)
	}
	if len(dependsOnParents) != 1 || dependsOnParents[0].ID != first.ID {
		t.Fatalf("second task's DEPENDS_ON parents = %+v, want [%s]", dependsOnParents, first.ID)
	}
}

func TestRewireDependsOn_RedirectsPendingAndBlocksReady(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	origin := newTieredOriginTask(t, store, ctx)
	now := time.Now().UTC()

	oldDecision := newRewireDecisionTask(now, origin.ProjectID, "decision", models.TaskStateCompleted)
	newDecision := newRewireDecisionTask(now, origin.ProjectID, "decision (re-gather v2)", models.TaskStateReady)
	execute := newRewireStepTask(now, origin.ProjectID, "tier-execute", "execute", models.TaskStateReady)
	verify := newRewireStepTask(now, origin.ProjectID, "tier-verify", "verify", models.TaskStatePending)
	pendingDep := newRewireStepTask(now, origin.ProjectID, "tier-pending-dep", "pending-dep", models.TaskStatePending)

	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: oldDecision},
		{Task: newDecision},
		{Task: execute, DependsOnID: oldDecision.ID},
		{Task: verify, DependsOnID: oldDecision.ID},
		{Task: pendingDep, DependsOnID: oldDecision.ID},
	}); err != nil {
		t.Fatalf("SpawnTieredContinuation() error = %v", err)
	}

	rewired, err := store.RewireDependsOn(ctx, oldDecision.ID, newDecision.ID)
	if err != nil {
		t.Fatalf("RewireDependsOn() error = %v", err)
	}
	if len(rewired) != 3 {
		t.Fatalf("rewired len = %d, want 3", len(rewired))
	}

	assertRewireExecuteBlocked(t, ctx, store, execute.ID, newDecision.ID)
	assertRewirePendingDepRedirected(t, ctx, store, pendingDep.ID, newDecision.ID)
	assertRewireVerifyPendingRedirected(t, ctx, store, verify.ID, newDecision.ID)
}

func newRewireDecisionTask(now time.Time, projectID, title string, state models.TaskState) models.Task {
	return newRewireStepTask(now, projectID, "tier-decision", title, state)
}

func newRewireStepTask(now time.Time, projectID, agentID, title string, state models.TaskState) models.Task {
	return models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   projectID,
		AgentID:     agentID,
		Title:       title,
		Description: "d",
		State:       state,
		Assignee:    models.TaskAssigneeSystem,
	}
}

func assertRewireExecuteBlocked(t *testing.T, ctx context.Context, store *Store, executeID, newDecisionID string) {
	t.Helper()
	executeParents, err := store.ListParentTasksByRelation(ctx, executeID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(execute) error = %v", err)
	}
	if len(executeParents) != 1 || executeParents[0].ID != newDecisionID {
		t.Fatalf("execute's DEPENDS_ON parents = %+v, want [%s]", executeParents, newDecisionID)
	}
	reloadedExecute, err := store.GetTask(ctx, executeID)
	if err != nil {
		t.Fatalf("GetTask(execute) error = %v", err)
	}
	if reloadedExecute.State != models.TaskStateBlocked {
		t.Fatalf("execute state = %s, want BLOCKED", reloadedExecute.State)
	}
}

func assertRewirePendingDepRedirected(t *testing.T, ctx context.Context, store *Store, pendingDepID, newDecisionID string) {
	t.Helper()
	reloaded, err := store.GetTask(ctx, pendingDepID)
	if err != nil {
		t.Fatalf("GetTask(pendingDep) error = %v", err)
	}
	if reloaded.State != models.TaskStatePending {
		t.Fatalf("pendingDep state = %s, want unchanged PENDING", reloaded.State)
	}
	parents, err := store.ListParentTasksByRelation(ctx, pendingDepID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(pendingDep) error = %v", err)
	}
	if len(parents) != 1 || parents[0].ID != newDecisionID {
		t.Fatalf("pendingDep's DEPENDS_ON parents = %+v, want [%s]", parents, newDecisionID)
	}
}

func assertRewireVerifyPendingRedirected(t *testing.T, ctx context.Context, store *Store, verifyID, newDecisionID string) {
	t.Helper()
	reloadedVerify, err := store.GetTask(ctx, verifyID)
	if err != nil {
		t.Fatalf("GetTask(verify) error = %v", err)
	}
	if reloadedVerify.State != models.TaskStatePending {
		t.Fatalf("verify state = %s, want unchanged PENDING", reloadedVerify.State)
	}
	verifyParents, err := store.ListParentTasksByRelation(ctx, verifyID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(verify) error = %v", err)
	}
	if len(verifyParents) != 1 || verifyParents[0].ID != newDecisionID {
		t.Fatalf("verify's DEPENDS_ON parents = %+v, want [%s]", verifyParents, newDecisionID)
	}
}
