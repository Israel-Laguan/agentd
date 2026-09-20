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

func TestSpawnTieredContinuation_OriginStateValidation(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, state string
		wantState   models.TaskState
		wantErr     bool
	}{
		{name: "ready is reblocked", state: string(models.TaskStateReady), wantState: models.TaskStateBlocked},
		{name: "pending is rejected", state: string(models.TaskStatePending), wantState: models.TaskStatePending, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			origin := newTieredOriginTask(t, store, ctx)
			var err error
			if tc.wantErr {
				_, err = store.db.ExecContext(ctx, `UPDATE tasks SET state = 'PENDING' WHERE id = ?`, origin.ID)
			} else {
				_, err = store.UpdateTaskState(ctx, origin.ID, origin.UpdatedAt, models.TaskState(tc.state))
			}
			if err != nil {
				t.Fatalf("set origin state: %v", err)
			}
			child := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-context", "continuation", models.TaskStateReady)
			_, err = store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{{Task: child}})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, tc.wantErr)
			}
			current, err := store.GetTask(ctx, origin.ID)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			want := tc.wantState
			if current.State != want {
				t.Fatalf("origin state = %s, want %s", current.State, want)
			}
		})
	}
}

func TestRewireDependsOn_RedirectsPendingAndBlocksReady(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	origin := newTieredOriginTask(t, store, ctx)
	now := time.Now().UTC()

	oldDecision := newRewireDecisionTask(now, origin.ProjectID, "decision", models.TaskStateCompleted)
	newDecision := newRewireDecisionTask(now, origin.ProjectID, "decision (re-gather v2)", models.TaskStateReady)
	execute := newRewireStepTask(now, origin.ProjectID, "tier-execute", "execute", models.TaskStatePending)
	verify := newRewireStepTask(now, origin.ProjectID, "tier-verify", "verify", models.TaskStatePending)
	pendingDep := newRewireStepTask(now, origin.ProjectID, "tier-pending-dep", "pending-dep", models.TaskStatePending)
	queuedDep := newRewireStepTask(now, origin.ProjectID, "tier-queued-dep", "queued-dep", models.TaskStateQueued)
	idempotentDep := newRewireStepTask(now, origin.ProjectID, "tier-idempotent-dep", "idempotent-dep", models.TaskStatePending)

	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: oldDecision},
		{Task: newDecision},
		{Task: execute, DependsOnID: oldDecision.ID},
		{Task: verify, DependsOnID: oldDecision.ID},
		{Task: pendingDep, DependsOnID: oldDecision.ID},
		{Task: queuedDep, DependsOnID: oldDecision.ID},
		{Task: idempotentDep, DependsOnID: newDecision.ID},
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

	assertRewirePendingDepRedirected(t, ctx, store, execute.ID, newDecision.ID)
	assertRewirePendingDepRedirected(t, ctx, store, pendingDep.ID, newDecision.ID)
	assertRewireVerifyPendingRedirected(t, ctx, store, verify.ID, newDecision.ID)
	assertRewireParent(t, ctx, store, queuedDep.ID, oldDecision.ID)
	assertRewireParent(t, ctx, store, idempotentDep.ID, newDecision.ID)
}

func assertRewireParent(t *testing.T, ctx context.Context, store *Store, taskID, parentID string) {
	t.Helper()
	parents, err := store.ListParentTasksByRelation(ctx, taskID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation: %v", err)
	}
	if len(parents) != 1 || parents[0].ID != parentID {
		t.Fatalf("parents = %+v, want [%s]", parents, parentID)
	}
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
