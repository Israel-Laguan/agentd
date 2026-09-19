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
	origin, err := store.UpdateTaskState(ctx, tasks[0].ID, tasks[0].UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("UpdateTaskState(RUNNING) error = %v", err)
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
	if reloadedOrigin.State != models.TaskStateRunning {
		t.Fatalf("origin state = %s, want unchanged RUNNING", reloadedOrigin.State)
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

	oldDecision := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   origin.ProjectID,
		AgentID:     "tier-decision",
		Title:       "decision",
		Description: "d",
		State:       models.TaskStateCompleted,
		Assignee:    models.TaskAssigneeSystem,
	}
	newDecision := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   origin.ProjectID,
		AgentID:     "tier-decision",
		Title:       "decision (re-gather v2)",
		Description: "d",
		State:       models.TaskStateReady,
		Assignee:    models.TaskAssigneeSystem,
	}
	// execute is READY (as it would be immediately after the old decision
	// completed and UnlockReadyChildren promoted it).
	execute := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   origin.ProjectID,
		AgentID:     "tier-execute",
		Title:       "execute",
		Description: "d",
		State:       models.TaskStateReady,
		Assignee:    models.TaskAssigneeSystem,
	}
	// verify is still PENDING, depending on execute — untouched by this
	// rewire since its DEPENDS_ON parent is execute, not oldDecision.
	verify := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   origin.ProjectID,
		AgentID:     "tier-verify",
		Title:       "verify",
		Description: "d",
		State:       models.TaskStatePending,
		Assignee:    models.TaskAssigneeSystem,
	}

	if _, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{
		{Task: oldDecision},
		{Task: newDecision},
		{Task: execute, DependsOnID: oldDecision.ID},
		{Task: verify, DependsOnID: execute.ID},
	}); err != nil {
		t.Fatalf("SpawnTieredContinuation() error = %v", err)
	}

	rewired, err := store.RewireDependsOn(ctx, oldDecision.ID, newDecision.ID)
	if err != nil {
		t.Fatalf("RewireDependsOn() error = %v", err)
	}
	if len(rewired) != 1 || rewired[0].ID != execute.ID {
		t.Fatalf("rewired = %+v, want exactly [execute]", rewired)
	}

	// execute must now depend on newDecision, not oldDecision, and must
	// have been demoted to BLOCKED so it cannot run against the stale pack.
	executeParents, err := store.ListParentTasksByRelation(ctx, execute.ID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(execute) error = %v", err)
	}
	if len(executeParents) != 1 || executeParents[0].ID != newDecision.ID {
		t.Fatalf("execute's DEPENDS_ON parents = %+v, want [%s]", executeParents, newDecision.ID)
	}
	reloadedExecute, err := store.GetTask(ctx, execute.ID)
	if err != nil {
		t.Fatalf("GetTask(execute) error = %v", err)
	}
	if reloadedExecute.State != models.TaskStateBlocked {
		t.Fatalf("execute state = %s, want BLOCKED", reloadedExecute.State)
	}

	// verify is untouched: still PENDING, still depending on execute.
	reloadedVerify, err := store.GetTask(ctx, verify.ID)
	if err != nil {
		t.Fatalf("GetTask(verify) error = %v", err)
	}
	if reloadedVerify.State != models.TaskStatePending {
		t.Fatalf("verify state = %s, want unchanged PENDING", reloadedVerify.State)
	}
	verifyParents, err := store.ListParentTasksByRelation(ctx, verify.ID, models.TaskRelationDependsOn)
	if err != nil {
		t.Fatalf("ListParentTasksByRelation(verify) error = %v", err)
	}
	if len(verifyParents) != 1 || verifyParents[0].ID != execute.ID {
		t.Fatalf("verify's DEPENDS_ON parents = %+v, want [%s]", verifyParents, execute.ID)
	}
}
