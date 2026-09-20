package kanban

import (
	"context"
	"strings"
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
			child := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-context", "continuation", models.TaskStateReady)
			if tc.wantErr {
				_, err = store.db.ExecContext(ctx, `UPDATE tasks SET state = 'PENDING' WHERE id = ?`, origin.ID)
			} else {
				_, err = store.UpdateTaskState(ctx, origin.ID, origin.UpdatedAt, models.TaskState(tc.state))
			}
			if err != nil {
				t.Fatalf("set origin state: %v", err)
			}
			_, err = store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{{Task: child}})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, tc.wantErr)
			}
			if tc.wantErr {
				_, err = store.GetTask(ctx, child.ID)
				if err == nil {
					t.Fatalf("child task %s should not exist after rejection", child.ID)
				}
			} else {
				_, err = store.GetTask(ctx, child.ID)
				if err != nil {
					t.Fatalf("child task %s should exist after success: %v", child.ID, err)
				}
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

func TestSpawnTieredContinuation_ChildValidation(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		children func(origin models.Task) []models.TieredContinuationTask
		wantErr  string
	}{
		{name: "duplicate child IDs", children: duplicateChildIDsCase, wantErr: "duplicate child ID"},
		{name: "unknown dependency", children: unknownDependencyCase, wantErr: "task not found"},
		{name: "self dependency", children: selfDependencyCase, wantErr: "cannot depend on itself"},
		{name: "ready dependent child", children: readyDependentChildCase, wantErr: "cannot be READY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			origin := newTieredOriginTask(t, store, ctx)
			children := tc.children(origin)
			_, err := store.SpawnTieredContinuation(ctx, origin.ID, children)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
			for _, child := range children {
				if _, getErr := store.GetTask(ctx, child.Task.ID); getErr == nil {
					t.Fatalf("child task %s persisted after rejection", child.Task.ID)
				}
			}
		})
	}
}

func duplicateChildIDsCase(origin models.Task) []models.TieredContinuationTask {
	id := uuid.NewString()
	first := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-context", "first", models.TaskStateReady)
	first.ID = id
	second := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-decision", "second", models.TaskStatePending)
	second.ID = id
	return []models.TieredContinuationTask{{Task: first}, {Task: second}}
}

func unknownDependencyCase(origin models.Task) []models.TieredContinuationTask {
	child := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-decision", "child", models.TaskStatePending)
	return []models.TieredContinuationTask{{Task: child, DependsOnID: uuid.NewString()}}
}

func selfDependencyCase(origin models.Task) []models.TieredContinuationTask {
	child := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-decision", "child", models.TaskStatePending)
	return []models.TieredContinuationTask{{Task: child, DependsOnID: child.ID}}
}

func readyDependentChildCase(origin models.Task) []models.TieredContinuationTask {
	parent := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-context", "parent", models.TaskStateReady)
	child := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-decision", "child", models.TaskStateReady)
	return []models.TieredContinuationTask{{Task: parent}, {Task: child, DependsOnID: parent.ID}}
}

func TestSpawnTieredContinuation_IdempotencyKeyReplaysBatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	origin := newTieredOriginTask(t, store, ctx)
	child := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-context", "child", models.TaskStateReady)
	input := []models.TieredContinuationTask{{Task: child, IdempotencyKey: "verify-1"}}

	first, err := store.SpawnTieredContinuation(ctx, origin.ID, input)
	if err != nil {
		t.Fatalf("first SpawnTieredContinuation() error = %v", err)
	}
	second, err := store.SpawnTieredContinuation(ctx, origin.ID, input)
	if err != nil {
		t.Fatalf("replay SpawnTieredContinuation() error = %v", err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
		t.Fatalf("replay returned %+v, first returned %+v; want same child", second, first)
	}
	spawned, err := store.ListChildTasksByRelation(ctx, origin.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation() error = %v", err)
	}
	if len(spawned) != 1 {
		t.Fatalf("len(spawned) = %d, want 1", len(spawned))
	}
}

func TestSpawnTieredContinuation_IdempotencyKeyMismatchedChild(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	origin := newTieredOriginTask(t, store, ctx)
	child := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-context", "child", models.TaskStateReady)
	input := []models.TieredContinuationTask{{Task: child, IdempotencyKey: "verify-1"}}

	first, err := store.SpawnTieredContinuation(ctx, origin.ID, input)
	if err != nil {
		t.Fatalf("first SpawnTieredContinuation() error = %v", err)
	}

	differentChild := newRewireStepTask(time.Now().UTC(), origin.ProjectID, "tier-context", "child-v2", models.TaskStateReady)
	differentInput := []models.TieredContinuationTask{{Task: differentChild, IdempotencyKey: "verify-1"}}
	second, err := store.SpawnTieredContinuation(ctx, origin.ID, differentInput)
	if err != nil {
		t.Fatalf("replay SpawnTieredContinuation() error = %v", err)
	}

	if len(second) != 1 || second[0].ID != first[0].ID {
		t.Fatalf("replay returned %+v, want same as first %+v", second, first)
	}
	if _, err := store.GetTask(ctx, differentChild.ID); err == nil {
		t.Fatalf("different child %s should not exist after replay", differentChild.ID)
	}
	spawned, err := store.ListChildTasksByRelation(ctx, origin.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation() error = %v", err)
	}
	if len(spawned) != 1 {
		t.Fatalf("len(spawned) = %d, want 1", len(spawned))
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
	readyDepPending := newRewireStepTask(now, origin.ProjectID, "tier-ready-dep", "ready-dep", models.TaskStatePending)
	blockedDepPending := newRewireStepTask(now, origin.ProjectID, "tier-blocked-dep", "blocked-dep", models.TaskStatePending)
	queuedDep := newRewireStepTask(now, origin.ProjectID, "tier-queued-dep", "queued-dep", models.TaskStateQueued)
	unrelatedDep := newRewireStepTask(now, origin.ProjectID, "tier-unrelated-dep", "unrelated-dep", models.TaskStatePending)

	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: oldDecision},
		{Task: newDecision},
		{Task: execute, DependsOnID: oldDecision.ID},
		{Task: verify, DependsOnID: oldDecision.ID},
		{Task: pendingDep, DependsOnID: oldDecision.ID},
		{Task: readyDepPending, DependsOnID: oldDecision.ID},
		{Task: blockedDepPending, DependsOnID: oldDecision.ID},
		{Task: queuedDep, DependsOnID: oldDecision.ID},
		{Task: unrelatedDep, DependsOnID: newDecision.ID},
	}); err != nil {
		t.Fatalf("SpawnTieredContinuation() error = %v", err)
	}
	readyDep, blockedDep := promoteReadyAndBlockedDependents(t, ctx, store, readyDepPending, blockedDepPending)

	rewired, err := store.RewireDependsOn(ctx, oldDecision.ID, newDecision.ID)
	if err != nil {
		t.Fatalf("RewireDependsOn() error = %v", err)
	}
	if len(rewired) != 5 {
		t.Fatalf("rewired len = %d, want 5", len(rewired))
	}

	for _, child := range []struct{ id, label string }{
		{execute.ID, "execute"}, {pendingDep.ID, "pendingDep"}, {verify.ID, "verify"},
	} {
		assertRewirePendingRedirected(t, ctx, store, child.id, child.label, newDecision.ID)
	}
	assertRewireReadyDepBlocked(t, ctx, store, readyDep.ID, newDecision.ID)
	assertRewireBlockedDepRedirected(t, ctx, store, blockedDep.ID, newDecision.ID)
	assertRewireParent(t, ctx, store, queuedDep.ID, oldDecision.ID)
	assertRewireParent(t, ctx, store, unrelatedDep.ID, newDecision.ID)
}

func promoteReadyAndBlockedDependents(t *testing.T, ctx context.Context, store *Store, readyDepPending, blockedDepPending models.Task) (models.Task, models.Task) {
	t.Helper()
	readyTmp, err := store.GetTask(ctx, readyDepPending.ID)
	if err != nil {
		t.Fatalf("GetTask(readyDep): %v", err)
	}
	readyDepState, err := store.UpdateTaskState(ctx, readyTmp.ID, readyTmp.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState(readyDep READY): %v", err)
	}
	readyDep := *readyDepState
	blockedTmp, err := store.GetTask(ctx, blockedDepPending.ID)
	if err != nil {
		t.Fatalf("GetTask(blockedDep): %v", err)
	}
	blockedReady, err := store.UpdateTaskState(ctx, blockedTmp.ID, blockedTmp.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState(blockedDep READY): %v", err)
	}
	blockedDepState, err := store.UpdateTaskState(ctx, blockedReady.ID, blockedReady.UpdatedAt, models.TaskStateBlocked)
	if err != nil {
		t.Fatalf("UpdateTaskState(blockedDep BLOCKED): %v", err)
	}
	return readyDep, *blockedDepState
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

func assertRewirePendingRedirected(t *testing.T, ctx context.Context, store *Store, taskID, label, newDecisionID string) {
	t.Helper()
	reloaded, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask(%s) error = %v", label, err)
	}
	if reloaded.State != models.TaskStatePending {
		t.Fatalf("%s state = %s, want unchanged PENDING", label, reloaded.State)
	}
	parents, err := store.ListParentTasksByRelation(ctx, taskID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(%s) error = %v", label, err)
	}
	if len(parents) != 1 || parents[0].ID != newDecisionID {
		t.Fatalf("%s's DEPENDS_ON parents = %+v, want [%s]", label, parents, newDecisionID)
	}
}

func assertRewireReadyDepBlocked(t *testing.T, ctx context.Context, store *Store, readyDepID, newDecisionID string) {
	t.Helper()
	reloaded, err := store.GetTask(ctx, readyDepID)
	if err != nil {
		t.Fatalf("GetTask(readyDep) error = %v", err)
	}
	if reloaded.State != models.TaskStateBlocked {
		t.Fatalf("readyDep state = %s, want BLOCKED after rewire", reloaded.State)
	}
	parents, err := store.ListParentTasksByRelation(ctx, readyDepID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(readyDep) error = %v", err)
	}
	if len(parents) != 1 || parents[0].ID != newDecisionID {
		t.Fatalf("readyDep's DEPENDS_ON parents = %+v, want [%s]", parents, newDecisionID)
	}
}

func assertRewireBlockedDepRedirected(t *testing.T, ctx context.Context, store *Store, blockedDepID, newDecisionID string) {
	t.Helper()
	reloaded, err := store.GetTask(ctx, blockedDepID)
	if err != nil {
		t.Fatalf("GetTask(blockedDep) error = %v", err)
	}
	if reloaded.State != models.TaskStateBlocked {
		t.Fatalf("blockedDep state = %s, want unchanged BLOCKED", reloaded.State)
	}
	parents, err := store.ListParentTasksByRelation(ctx, blockedDepID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(blockedDep) error = %v", err)
	}
	if len(parents) != 1 || parents[0].ID != newDecisionID {
		t.Fatalf("blockedDep's DEPENDS_ON parents = %+v, want [%s]", parents, newDecisionID)
	}
}
