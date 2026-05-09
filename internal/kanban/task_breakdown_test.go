package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestBlockTaskWithSubtasksCreatesReadyChildrenAndBlocksParent(t *testing.T) {
	store := newTestStore(t)
	parent := seedTestTask(t, store, "parent", models.TaskStateRunning)

	blocked, children, err := store.BlockTaskWithSubtasks(context.Background(), parent.ID, parent.UpdatedAt, []models.DraftTask{
		{Title: "child one", Description: "first child"},
		{Title: "child two", Description: "second child", Assignee: models.TaskAssigneeHuman},
	})
	if err != nil {
		t.Fatalf("BlockTaskWithSubtasks() error = %v", err)
	}
	if blocked.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", blocked.State)
	}
	if len(children) != 2 {
		t.Fatalf("children = %d, want 2", len(children))
	}
	if children[0].State != models.TaskStateReady || children[1].State != models.TaskStateReady {
		t.Fatalf("children states = %s, %s; want READY, READY", children[0].State, children[1].State)
	}
	if children[1].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("child assignee = %s, want HUMAN", children[1].Assignee)
	}
	assertRelationCount(t, store, parent.ID, 2)
}

func TestBlockedParentResumesAfterAllChildrenComplete(t *testing.T) {
	store := newTestStore(t)
	parent := seedTestTask(t, store, "parent", models.TaskStateRunning)
	blocked, children, err := store.BlockTaskWithSubtasks(context.Background(), parent.ID, parent.UpdatedAt, []models.DraftTask{
		{Title: "child one"},
		{Title: "child two"},
	})
	if err != nil {
		t.Fatalf("BlockTaskWithSubtasks() error = %v", err)
	}

	firstRunning, err := store.UpdateTaskState(context.Background(), children[0].ID, children[0].UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("start first child: %v", err)
	}
	if _, err := store.UpdateTaskResult(context.Background(), firstRunning.ID, firstRunning.UpdatedAt, models.TaskResult{Success: true}); err != nil {
		t.Fatalf("complete first child: %v", err)
	}
	parentAfterOne, err := store.GetTask(context.Background(), blocked.ID)
	if err != nil {
		t.Fatalf("get parent after one child: %v", err)
	}
	if parentAfterOne.State != models.TaskStateBlocked {
		t.Fatalf("parent state after one child = %s, want BLOCKED", parentAfterOne.State)
	}

	secondRunning, err := store.UpdateTaskState(context.Background(), children[1].ID, children[1].UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("start second child: %v", err)
	}
	if _, err := store.UpdateTaskResult(context.Background(), secondRunning.ID, secondRunning.UpdatedAt, models.TaskResult{Success: true}); err != nil {
		t.Fatalf("complete second child: %v", err)
	}
	parentAfterAll, err := store.GetTask(context.Background(), blocked.ID)
	if err != nil {
		t.Fatalf("get parent after all children: %v", err)
	}
	if parentAfterAll.State != models.TaskStateReady {
		t.Fatalf("parent state after all children = %s, want READY", parentAfterAll.State)
	}
}

func assertRelationCount(t *testing.T, store *Store, parentID string, want int) {
	t.Helper()
	var got int
	if err := store.db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM task_relations WHERE parent_task_id = ?`, parentID).Scan(&got); err != nil {
		t.Fatalf("count task relations: %v", err)
	}
	if got != want {
		t.Fatalf("relation count = %d, want %d", got, want)
	}
}

func seedTestTask(t *testing.T, store *Store, title string, state models.TaskState) models.Task {
	t.Helper()
	now := time.Now().UTC()
	project := models.Project{
		BaseEntity:    models.BaseEntity{ID: "project-" + title, CreatedAt: now, UpdatedAt: now},
		Name:          "test project",
		OriginalInput: "test",
		WorkspacePath: "workspace-" + title,
		Status:        "ACTIVE",
	}
	if _, err := store.db.ExecContext(context.Background(), `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		project.ID, project.Name, project.OriginalInput, project.WorkspacePath, project.Status,
		formatTime(now), formatTime(now)); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-" + title, CreatedAt: now, UpdatedAt: now},
		ProjectID:   project.ID,
		AgentID:     defaultAgentID,
		Title:       title,
		Description: "test task",
		State:       state,
		Assignee:    models.TaskAssigneeSystem,
	}
	if err := insertTask(context.Background(), store.db, task.Title, task); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	return task
}
