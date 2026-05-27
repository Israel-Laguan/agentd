package kanban

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

// TestMaterializeWithAgentID verifies that DraftTask.AgentID is carried
// through to the materialized Task row, eliminating the need for a
// separate assign call after materialize.
func TestMaterializeWithAgentID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seedProfile(t, store, ctx, "default")
	seedProfile(t, store, ctx, "researcher")

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "pre-assign",
		Tasks: []models.DraftTask{
			{TempID: "t1", Title: "Research", AgentID: "researcher"},
			{TempID: "t2", Title: "Implement"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	byTitle := tasksByTitle(tasks)
	if byTitle["Research"].AgentID != "researcher" {
		t.Fatalf("Research agent_id = %q, want researcher", byTitle["Research"].AgentID)
	}
	if byTitle["Implement"].AgentID != defaultAgentID {
		t.Fatalf("Implement agent_id = %q, want %s", byTitle["Implement"].AgentID, defaultAgentID)
	}
}

// TestMaterializeWithUnknownAgentIDFails verifies that referencing a
// non-existent agent profile in DraftTask.AgentID fails at materialize
// time with ErrAgentProfileNotFound.
func TestMaterializeWithUnknownAgentIDFails(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "bad-agent",
		Tasks: []models.DraftTask{
			{TempID: "t1", Title: "Ghost", AgentID: "nonexistent"},
		},
	})
	if err == nil {
		t.Fatal("MaterializePlan() expected error for unknown agent_id")
	}
	if !errors.Is(err, models.ErrAgentProfileNotFound) {
		t.Fatalf("MaterializePlan() error = %v, want ErrAgentProfileNotFound", err)
	}
}

// TestAssignReadyTaskNoConflict confirms that assigning a READY task
// (not yet claimed by a worker) succeeds without STATE_CONFLICT.
func TestAssignReadyTaskNoConflict(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seedProfile(t, store, ctx, "default")
	seedProfile(t, store, ctx, "researcher")

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "assign-ready",
		Tasks:       []models.DraftTask{{TempID: "t1", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	task := tasks[0]

	updated, err := store.AssignTaskAgent(ctx, task.ID, task.UpdatedAt, "researcher")
	if err != nil {
		t.Fatalf("AssignTaskAgent() error = %v", err)
	}
	if updated.AgentID != "researcher" {
		t.Fatalf("agent_id = %q, want researcher", updated.AgentID)
	}
}

// TestAssignRunningTaskConflict confirms that assigning a RUNNING task
// returns ErrStateConflict (existing behavior preserved).
func TestAssignRunningTaskConflict(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seedProfile(t, store, ctx, "default")
	seedProfile(t, store, ctx, "researcher")

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "assign-running",
		Tasks:       []models.DraftTask{{TempID: "t1", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	task := tasks[0]

	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) == 0 {
		t.Fatalf("ClaimNextReadyTasks() tasks=%d err=%v", len(claimed), err)
	}
	running, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, 99999)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}

	_, err = store.AssignTaskAgent(ctx, running.ID, running.UpdatedAt, "researcher")
	if !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("AssignTaskAgent() error = %v, want ErrStateConflict", err)
	}
	_ = task // keep linter happy
}

// TestPreAssignedTaskClaimableAndRoutedCorrectly verifies that a task
// materialized with an explicit agent_id is claimed by the dispatch
// loop and retains the pre-assigned agent_id through the claim.
func TestPreAssignedTaskClaimableAndRoutedCorrectly(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seedProfile(t, store, ctx, "default")
	seedProfile(t, store, ctx, "researcher")

	_, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "routed",
		Tasks: []models.DraftTask{
			{TempID: "t1", Title: "ResearchTask", AgentID: "researcher"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	claimed, err := store.ClaimNextReadyTasks(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed task, got %d", len(claimed))
	}
	if claimed[0].AgentID != "researcher" {
		t.Fatalf("claimed task agent_id = %q, want researcher", claimed[0].AgentID)
	}
	if claimed[0].Title != "ResearchTask" {
		t.Fatalf("claimed task title = %q, want ResearchTask", claimed[0].Title)
	}
}

// TestSubtaskInheritsAgentID verifies that DraftTask.AgentID is honored
// when creating subtasks via BlockTaskWithSubtasks.
func TestSubtaskInheritsAgentID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seedProfile(t, store, ctx, "default")
	seedProfile(t, store, ctx, "qa")

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "subtask-agent",
		Tasks:       []models.DraftTask{{TempID: "t1", Title: "Parent"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	parent := tasks[0]

	claimed, _ := store.ClaimNextReadyTasks(ctx, 1)
	running, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, 12345)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}

	_, children, err := store.BlockTaskWithSubtasks(ctx, running.ID, running.UpdatedAt, []models.DraftTask{
		{Title: "QA check", AgentID: "qa"},
		{Title: "Default check"},
	})
	if err != nil {
		t.Fatalf("BlockTaskWithSubtasks() error = %v", err)
	}

	childByTitle := tasksByTitle(children)
	if childByTitle["QA check"].AgentID != "qa" {
		t.Fatalf("QA check agent_id = %q, want qa", childByTitle["QA check"].AgentID)
	}
	if childByTitle["Default check"].AgentID != defaultAgentID {
		t.Fatalf("Default check agent_id = %q, want %s", childByTitle["Default check"].AgentID, defaultAgentID)
	}
	_ = parent // keep linter happy
}

// TestBlockTaskWithSubtasks_UnknownAgentIDFails verifies that creating
// subtasks with a non-existent agent_id is rejected.
func TestBlockTaskWithSubtasks_UnknownAgentIDFails(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seedProfile(t, store, ctx, "default")

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "subtask-bad-agent",
		Tasks:       []models.DraftTask{{TempID: "t1", Title: "Parent"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	claimed, _ := store.ClaimNextReadyTasks(ctx, 1)
	running, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, 12345)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}

	_, _, err = store.BlockTaskWithSubtasks(ctx, running.ID, running.UpdatedAt, []models.DraftTask{
		{Title: "Ghost subtask", AgentID: "nonexistent"},
	})
	if !errors.Is(err, models.ErrAgentProfileNotFound) {
		t.Fatalf("BlockTaskWithSubtasks() error = %v, want ErrAgentProfileNotFound", err)
	}
	_ = tasks
}

// TestAppendTasksToProject_UnknownAgentIDFails verifies that appending
// tasks with a non-existent agent_id is rejected.
func TestAppendTasksToProject_UnknownAgentIDFails(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seedProfile(t, store, ctx, "default")

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "append-bad-agent",
		Tasks:       []models.DraftTask{{TempID: "t1", Title: "Parent"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	_, err = store.AppendTasksToProject(ctx, tasks[0].ProjectID, tasks[0].ID, []models.DraftTask{
		{Title: "Ghost follow-up", AgentID: "nonexistent"},
	})
	if !errors.Is(err, models.ErrAgentProfileNotFound) {
		t.Fatalf("AppendTasksToProject() error = %v, want ErrAgentProfileNotFound", err)
	}
}

func seedProfile(t *testing.T, store *Store, ctx context.Context, id string) {
	t.Helper()
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: id, Name: id, Provider: "openai", Model: "gpt-4o-mini",
	}); err != nil {
		t.Fatalf("seed profile %q: %v", id, err)
	}
}
