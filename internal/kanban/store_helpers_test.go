package kanban

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"agentd/internal/models"
)

func samplePlan() models.DraftPlan {
	return models.DraftPlan{
		ProjectName: "phase one",
		Description: "build project skeleton",
		Tasks: []models.DraftTask{
			{TempID: "a", Title: "A"},
			{TempID: "b", Title: "B", DependsOn: []string{"a"}},
			{TempID: "c", Title: "C", DependsOn: []string{"a"}},
			{TempID: "d", Title: "D", DependsOn: []string{"b", "c"}},
			{TempID: "e", Title: "E"},
		},
	}
}

func tasksByTitle(tasks []models.Task) map[string]models.Task {
	byTitle := make(map[string]models.Task, len(tasks))
	for _, task := range tasks {
		byTitle[task.Title] = task
	}
	return byTitle
}

func assertInitialTaskStates(t *testing.T, byTitle map[string]models.Task) {
	t.Helper()
	assertTaskState(t, byTitle, "A", models.TaskStateReady)
	assertTaskState(t, byTitle, "E", models.TaskStateReady)
	assertTaskState(t, byTitle, "B", models.TaskStatePending)
	assertTaskState(t, byTitle, "C", models.TaskStatePending)
	assertTaskState(t, byTitle, "D", models.TaskStatePending)
}

func assertTaskState(t *testing.T, byTitle map[string]models.Task, title string, want models.TaskState) {
	t.Helper()
	if byTitle[title].State != want {
		t.Fatalf("task %s state = %s, want %s", title, byTitle[title].State, want)
	}
}

func assertRunningMetadata(t *testing.T, task *models.Task) {
	t.Helper()
	if task.StartedAt == nil {
		t.Fatal("expected started_at to be set when task enters RUNNING")
	}
	if task.LastHeartbeat != nil {
		t.Fatalf("expected last heartbeat to start nil, got %v", task.LastHeartbeat)
	}
}

func transition(
	t *testing.T,
	store *Store,
	ctx context.Context,
	task models.Task,
	next models.TaskState,
) *models.Task {
	t.Helper()
	updated, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, next)
	if err != nil {
		t.Fatalf("%s -> %s error = %v", task.State, next, err)
	}
	return updated
}

func completeTask(t *testing.T, store *Store, ctx context.Context, task models.Task) *models.Task {
	t.Helper()
	updated, err := store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{Success: true})
	if err != nil {
		t.Fatalf("UpdateTaskResult() error = %v", err)
	}
	return updated
}

func claimConcurrently(store *Store, ctx context.Context) ([][]models.Task, []error) {
	start := make(chan struct{})
	claimed := make([][]models.Task, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range claimed {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			claimed[i], errs[i] = store.ClaimNextReadyTasks(ctx, 1)
		}(i)
	}
	close(start)
	wg.Wait()
	return claimed, errs
}

func assertAtomicClaimResult(t *testing.T, claimed [][]models.Task, errs []error) {
	t.Helper()
	total := 0
	emptyClaims := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("ClaimNextReadyTasks(%d) error = %v", i, err)
		}
		total += len(claimed[i])
		if len(claimed[i]) == 0 {
			emptyClaims++
		}
	}
	if total != 1 || emptyClaims != 1 {
		t.Fatalf("claims = %#v, want one claimed task and one empty claim", claimed)
	}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("table %s count = %d, want %d", table, got, want)
	}
}
