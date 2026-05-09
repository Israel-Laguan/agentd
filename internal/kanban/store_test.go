package kanban

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestMaterializeAndClaim(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tasks := materializeClaimPlan(t, store, ctx)
	assertCount(t, store.db, "projects", 1)
	assertCount(t, store.db, "tasks", 5)
	assertCount(t, store.db, "task_relations", 4)

	byTitle := tasksByTitle(tasks)
	assertInitialTaskStates(t, byTitle)
	claimed := assertClaimableTitles(t, store, ctx, "A", "E")

	updatedA := transition(t, store, ctx, claimed["A"], models.TaskStateRunning)
	assertRunningMetadata(t, updatedA)
	completeTask(t, store, ctx, *updatedA)

	assertClaimableTitles(t, store, ctx, "B", "C")
}

func materializeClaimPlan(t *testing.T, store *Store, ctx context.Context) []models.Task {
	t.Helper()
	project, tasks, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	if project == nil || len(tasks) != 5 {
		t.Fatalf("unexpected materialize result: project=%v tasks=%d", project != nil, len(tasks))
	}
	return tasks
}

func assertClaimableTitles(t *testing.T, store *Store, ctx context.Context, titles ...string) map[string]models.Task {
	t.Helper()
	ready, err := store.ClaimNextReadyTasks(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	readyByTitle := make(map[string]models.Task, len(ready))
	for _, task := range ready {
		if task.State != models.TaskStateQueued {
			t.Fatalf("claimed task %s state = %s, want QUEUED", task.Title, task.State)
		}
		readyByTitle[task.Title] = task
	}
	for _, title := range titles {
		if _, ok := readyByTitle[title]; !ok {
			t.Fatalf("expected ready queue to include %s, got %#v", title, readyByTitle)
		}
	}
	return readyByTitle
}

func TestUniqueWorkspacePath(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	projectID1 := "project-1"
	projectID2 := "project-2"
	workspace := "same-workspace"

	_, err := store.db.ExecContext(ctx, `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'ACTIVE', ?, ?)`, projectID1, "p1", "input", workspace, now, now)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'ACTIVE', ?, ?)`, projectID2, "p2", "input", workspace, now, now)
	if err == nil {
		t.Fatal("expected UNIQUE constraint violation for workspace_path")
	}
}

func TestCircularDependencyRejected(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "cycle",
		Tasks: []models.DraftTask{
			{TempID: "a", Title: "A", DependsOn: []string{"b"}},
			{TempID: "b", Title: "B", DependsOn: []string{"a"}},
		},
	})
	if !errors.Is(err, models.ErrCircularDependency) {
		t.Fatalf("MaterializePlan() error = %v, want ErrCircularDependency", err)
	}
	assertCount(t, store.db, "projects", 0)
	assertCount(t, store.db, "tasks", 0)
}

func TestOptimisticLockMismatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "lock",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	stale := tasks[0].UpdatedAt.Add(-time.Second)
	_, err = store.UpdateTaskState(ctx, tasks[0].ID, stale, models.TaskStateRunning)
	if !errors.Is(err, models.ErrOptimisticLock) {
		t.Fatalf("UpdateTaskState() error = %v, want ErrOptimisticLock", err)
	}
}

func TestUpdateTaskStateRejectsInvalidTransition(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "invalid transition",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	_, err = store.UpdateTaskState(ctx, tasks[0].ID, tasks[0].UpdatedAt, models.TaskStateCompleted)
	if !errors.Is(err, models.ErrInvalidStateTransition) {
		t.Fatalf("UpdateTaskState() error = %v, want ErrInvalidStateTransition", err)
	}
}

func TestUpdateTaskStateReturnsNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.UpdateTaskState(ctx, "missing-task", time.Now().UTC(), models.TaskStateReady)
	if !errors.Is(err, models.ErrTaskNotFound) {
		t.Fatalf("UpdateTaskState() error = %v, want ErrTaskNotFound", err)
	}
}

func TestClaimNextReadyTasksAtomic(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "claim lock",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	claimed, errs := claimConcurrently(store, ctx)
	assertAtomicClaimResult(t, claimed, errs)
	got, err := store.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.State != models.TaskStateQueued {
		t.Fatalf("task state = %s, want QUEUED", got.State)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	store := NewStore(db)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}
