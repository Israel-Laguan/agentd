package worker

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// seedBrokenDownParent materializes a parent with one plan dependent, breaks
// the parent into two subtasks, and returns the store, worker, parent and
// subtasks. withMarker false uses the plain breakdown, which writes no marker.
func seedBrokenDownParent(t *testing.T, withMarker bool) (*testutil.FakeKanbanStore, *Worker, models.Task, []models.Task) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "verify", Description: "d", Tasks: []models.DraftTask{
		{TempID: "a", Title: "Build the parser"},
		{TempID: "b", Title: "Document the parser", DependsOn: []string{"a"}},
	}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	parent := tasks[0]
	if parent.Title != "Build the parser" {
		parent = tasks[1]
	}
	running, err := store.MarkTaskRunning(ctx, parent.ID, parent.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	drafts := []models.DraftTask{{Title: "Lexer", SplitChild: true}, {Title: "Grammar", SplitChild: true}}
	var children []models.Task
	if withMarker {
		_, children, err = blockTaskWithBreakdown(ctx, store, *running, drafts)
	} else {
		_, children, err = store.BlockTaskWithSubtasks(ctx, running.ID, running.UpdatedAt, drafts)
	}
	if err != nil {
		t.Fatalf("breakdown: %v", err)
	}
	return store, &Worker{store: store}, *running, children
}

func completeTasks(t *testing.T, store *testutil.FakeKanbanStore, tasks []models.Task) {
	t.Helper()
	for _, task := range tasks {
		if _, err := store.UpdateTaskState(context.Background(), task.ID, task.UpdatedAt, models.TaskStateCompleted); err != nil {
			t.Fatalf("complete %s: %v", task.Title, err)
		}
	}
}

// A plan dependent is a BLOCKS child that waits on its parent until the parent
// completes. It must not count as an open subtask, or the parent could never
// finish.
func TestVerifyBreakdownComplete_IgnoresPlanDependents(t *testing.T) {
	t.Parallel()
	store, w, parent, subtasks := seedBrokenDownParent(t, true)
	completeTasks(t, store, subtasks)

	if err := w.VerifyBreakdownComplete(context.Background(), parent); err != nil {
		t.Fatalf("VerifyBreakdownComplete = %v, want nil once both subtasks completed", err)
	}
}

// Without a breakdown marker, an open subtask still refuses the finish.
func TestVerifyBreakdownComplete_MissingMarkerStillRefusesOpenSubtasks(t *testing.T) {
	t.Parallel()
	store, w, parent, subtasks := seedBrokenDownParent(t, false)
	completeTasks(t, store, subtasks[:1])

	if err := w.VerifyBreakdownComplete(context.Background(), parent); !errors.Is(err, models.ErrBreakdownIncomplete) {
		t.Fatalf("VerifyBreakdownComplete = %v with an open subtask and no marker, want ErrBreakdownIncomplete", err)
	}
}

// The roll-up uses the same child set as the finish gate: a plan dependent
// must not stop a parent whose split subtasks all completed from rolling up.
func TestTryRollUpBreakdown_IgnoresPlanDependents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, parent, subtasks := seedBrokenDownParent(t, true)
	completeTasks(t, store, subtasks)
	resumed := mustGetTask(t, store, parent.ID)
	running, err := store.MarkTaskRunning(ctx, resumed.ID, resumed.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning (state %s): %v", resumed.State, err)
	}
	w := NewWorker(store, nil, nil, nil, &mockEventSink{}, WorkerOptions{})

	handled, err := w.tryRollUpBreakdown(ctx, *running)
	if err != nil || !handled {
		t.Fatalf("tryRollUpBreakdown = %v, %v; want the parent rolled up", handled, err)
	}
	if got := mustGetTask(t, store, parent.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("parent state = %s, want COMPLETED", got)
	}
}
