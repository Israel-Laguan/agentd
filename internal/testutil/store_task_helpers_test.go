package testutil

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

func TestAppendTasksToProject_CreatesPendingChildren(t *testing.T) {
	s := NewFakeStore()
	ctx := context.Background()
	_, tasks, err := s.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "pending-children",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	created, err := s.AppendTasksToProject(ctx, tasks[0].ProjectID, tasks[0].ID, []models.DraftTask{{Title: "child"}})
	if err != nil {
		t.Fatalf("AppendTasksToProject: %v", err)
	}
	if len(created) != 1 || created[0].State != models.TaskStatePending {
		t.Fatalf("created = %+v, want one PENDING child", created)
	}
}

func TestPersistTieredDAG_RejectsDuplicateChildIDs(t *testing.T) {
	s := NewFakeStore()
	ctx := context.Background()
	parent := seedTieredRunningParent(t, s, ctx)
	children := []models.TieredDAGTask{
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "step-1"}, ProjectID: parent.ProjectID, State: models.TaskStateReady}},
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "step-1"}, ProjectID: parent.ProjectID, State: models.TaskStatePending}, DependsOnID: "step-1"},
	}
	if _, err := s.PersistTieredDAG(ctx, parent.ID, parent.UpdatedAt, children); !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("PersistTieredDAG with duplicate child ID = %v, want ErrStateConflict", err)
	}
	if _, ok := s.tasks["step-1"]; ok {
		t.Fatal("duplicate plan must not partially persist children")
	}
	if got := s.tasks[parent.ID].State; got != models.TaskStateRunning {
		t.Fatalf("parent state = %s, want RUNNING (plan rejected before mutation)", got)
	}
}

func TestPersistTieredDAG_RejectsExistingChildID(t *testing.T) {
	s := NewFakeStore()
	ctx := context.Background()
	parent := seedTieredRunningParent(t, s, ctx)
	if _, err := s.PersistTieredDAG(ctx, parent.ID, parent.UpdatedAt, []models.TieredDAGTask{
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: parent.ID}, ProjectID: parent.ProjectID}},
	}); !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("PersistTieredDAG with existing ID = %v, want ErrStateConflict", err)
	}
}

func TestPersistTieredDAG_RejectsUnknownDependsOnID(t *testing.T) {
	s := NewFakeStore()
	ctx := context.Background()
	parent := seedTieredRunningParent(t, s, ctx)
	children := []models.TieredDAGTask{
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "step-1"}, ProjectID: parent.ProjectID, State: models.TaskStateReady}},
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "step-2"}, ProjectID: parent.ProjectID, State: models.TaskStatePending}, DependsOnID: "missing"},
	}
	if _, err := s.PersistTieredDAG(ctx, parent.ID, parent.UpdatedAt, children); !errors.Is(err, models.ErrTaskNotFound) {
		t.Fatalf("PersistTieredDAG with unknown DependsOnID = %v, want ErrTaskNotFound", err)
	}
	if _, ok := s.tasks["step-1"]; ok {
		t.Fatal("invalid plan must not partially persist children")
	}
}

func TestPersistTieredDAG_AcceptsForwardFreeChain(t *testing.T) {
	s := NewFakeStore()
	ctx := context.Background()
	parent := seedTieredRunningParent(t, s, ctx)
	children := []models.TieredDAGTask{
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "step-1"}, ProjectID: parent.ProjectID, State: models.TaskStateReady}},
		{Task: models.Task{BaseEntity: models.BaseEntity{ID: "step-2"}, ProjectID: parent.ProjectID, State: models.TaskStatePending}, DependsOnID: "step-1"},
	}
	created, err := s.PersistTieredDAG(ctx, parent.ID, parent.UpdatedAt, children)
	if err != nil {
		t.Fatalf("PersistTieredDAG: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created = %d tasks, want 2", len(created))
	}
	if got := s.tasks[parent.ID].State; got != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", got)
	}
}

func seedTieredRunningParent(t *testing.T, s *FakeKanbanStore, ctx context.Context) models.Task {
	t.Helper()
	_, tasks, err := s.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "tiered-dag",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	running, err := s.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	return *running
}
