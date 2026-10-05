package worker

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// runningTask materializes one task and starts it, returning the RUNNING row
// the way a worker holds it when it decides to requeue.
func runningTask(t *testing.T, store *testutil.FakeKanbanStore) models.Task {
	t.Helper()
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "requeue",
		Tasks:       []models.DraftTask{{Title: "t", Description: "d"}},
	})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("MaterializePlan() = (%d tasks, %v)", len(tasks), err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}
	return *running
}

func stateOf(t *testing.T, store models.KanbanStore, id string) models.TaskState {
	t.Helper()
	task, err := store.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	return task.State
}

// B-018: a requeue whose version loses the race to another writer used to be
// dropped, leaving the task QUEUED/RUNNING until the reconciler's age
// threshold. The fake stores ignore the version, so this needs the contended
// store to be visible at all.
func TestRequeueTaskRetriesAfterLostOptimisticLock(t *testing.T) {
	t.Parallel()
	fake := testutil.NewFakeStore()
	running := runningTask(t, fake)
	store := testutil.NewContendedStateStore(fake, 1)

	if err := RequeueTask(context.Background(), store, running, models.TaskStateRunning); err != nil {
		t.Fatalf("RequeueTask() error = %v", err)
	}
	if got := stateOf(t, fake, running.ID); got != models.TaskStateReady {
		t.Fatalf("state = %s, want READY: the requeue was dropped", got)
	}
	if got := store.StateUpdateAttempts(); got != 2 {
		t.Fatalf("UpdateTaskState attempts = %d, want 2 (one lost, one retried)", got)
	}
}

func TestWorkerRequeueSurvivesLostOptimisticLock(t *testing.T) {
	t.Parallel()
	fake := testutil.NewFakeStore()
	running := runningTask(t, fake)
	w := NewWorker(testutil.NewContendedStateStore(fake, 1), nil, nil, nil, nil, WorkerOptions{})

	w.requeue(context.Background(), running, "")

	if got := stateOf(t, fake, running.ID); got != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", got)
	}
}

func TestRequeueTaskLeavesARowAnotherWriterMoved(t *testing.T) {
	t.Parallel()
	fake := testutil.NewFakeStore()
	running := runningTask(t, fake)
	if _, err := fake.UpdateTaskState(context.Background(), running.ID, running.UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}
	store := testutil.NewContendedStateStore(fake, 0)

	// running carries the pre-completion version, so the write loses the lock.
	if err := RequeueTask(context.Background(), store, running, models.TaskStateRunning); err != nil {
		t.Fatalf("RequeueTask() error = %v, want nil: the row is no longer ours", err)
	}
	if got := stateOf(t, fake, running.ID); got != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED left alone", got)
	}
	if got := store.StateUpdateAttempts(); got != 1 {
		t.Fatalf("attempts = %d, want 1: a moved row must not be retried", got)
	}
}

func TestRequeueTaskStopsChasingAHotRow(t *testing.T) {
	t.Parallel()
	fake := testutil.NewFakeStore()
	running := runningTask(t, fake)
	store := testutil.NewContendedStateStore(fake, 1000)

	err := RequeueTask(context.Background(), store, running, models.TaskStateRunning)
	if !errors.Is(err, models.ErrOptimisticLock) {
		t.Fatalf("RequeueTask() error = %v, want ErrOptimisticLock once the bound is hit", err)
	}
	// A literal, not requeueAttempts: the point is that the bound does not drift.
	if got := store.StateUpdateAttempts(); got != 3 {
		t.Fatalf("attempts = %d, want exactly 3", got)
	}
}
