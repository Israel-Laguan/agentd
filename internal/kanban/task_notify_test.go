package kanban

import (
	"context"
	"sync"
	"testing"
	"time"

	"agentd/internal/models"
)

type notifyRecorder struct {
	mu    sync.Mutex
	tasks []models.Task
}

func (r *notifyRecorder) add(task models.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks = append(r.tasks, task)
}

func (r *notifyRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks = nil
}

func (r *notifyRecorder) has(id, projectID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, task := range r.tasks {
		if task.ID == id && task.ProjectID == projectID {
			return true
		}
	}
	return false
}

func notifyingStore(t *testing.T) (*Store, *notifyRecorder) {
	t.Helper()
	rec := &notifyRecorder{}
	return newTestStore(t).WithTaskNotifier(rec.add), rec
}

type notifyCase func(t *testing.T, s *Store, rec *notifyRecorder) models.Task

// Every write path that moves a task row must name that task and its project
// to the notifier. Each case seeds, resets the recorder, performs the write and
// returns the task the notifier must name.
func TestStoreNotifiesEveryTaskWritePath(t *testing.T) {
	for _, group := range []map[string]notifyCase{dispatchNotifyCases(), stateNotifyCases(), recoveryNotifyCases()} {
		for name, act := range group {
			t.Run(name, func(t *testing.T) {
				s, rec := notifyingStore(t)
				want := act(t, s, rec)
				if !rec.has(want.ID, want.ProjectID) {
					t.Fatalf("notifier did not name task %s in project %s; got %+v", want.ID, want.ProjectID, rec.tasks)
				}
			})
		}
	}
}

func dispatchNotifyCases() map[string]notifyCase {
	ctx := context.Background()
	return map[string]notifyCase{
		"claim": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			materializeClaimPlan(t, s, ctx)
			rec.reset()
			claimed, err := s.ClaimNextReadyTasks(ctx, 1)
			must(t, err)
			return claimed[0]
		},
		"mark running": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			task := seedTestTask(t, s, "mark-running", models.TaskStateQueued)
			rec.reset()
			_, err := s.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 4321)
			must(t, err)
			return task
		},
		"retry increment": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			task := seedTestTask(t, s, "retry", models.TaskStateRunning)
			rec.reset()
			_, err := s.IncrementRetryCount(ctx, task.ID, task.UpdatedAt)
			must(t, err)
			return task
		},
		"project tasks ready": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			task := seedTestTask(t, s, "mark-ready", models.TaskStatePending)
			rec.reset()
			_, err := s.MarkProjectTasksReady(ctx, task.ProjectID)
			must(t, err)
			return task
		},
	}
}

func stateNotifyCases() map[string]notifyCase {
	ctx := context.Background()
	return map[string]notifyCase{
		"state update": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			task := seedTestTask(t, s, "state-update", models.TaskStateRunning)
			rec.reset()
			_, err := s.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked)
			must(t, err)
			return task
		},
		"patch": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			task := seedTestTask(t, s, "patch", models.TaskStateRunning)
			rec.reset()
			next := models.TaskStateBlocked
			_, err := s.UpdateTaskPatch(ctx, task.ID, task.UpdatedAt, &next, nil)
			must(t, err)
			return task
		},
		"result": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			task := seedTestTask(t, s, "result", models.TaskStateRunning)
			rec.reset()
			_, err := s.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{Success: true, Payload: "ok"})
			must(t, err)
			return task
		},
		"block with subtasks": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			task := seedTestTask(t, s, "block", models.TaskStateRunning)
			rec.reset()
			_, _, err := s.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{Title: "child", Description: "d"}})
			must(t, err)
			return task
		},
	}
}

func recoveryNotifyCases() map[string]notifyCase {
	ctx := context.Background()
	return map[string]notifyCase{
		"tiered origin": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			origin := seedTieredOrigin(t, s, ctx, "tiered-notify")
			rec.reset()
			_, err := s.CompleteTieredOrigin(ctx, origin.ID, origin.UpdatedAt, models.TaskResult{Success: true, Payload: "ok"})
			must(t, err)
			return origin
		},
		"ghost reconcile": func(t *testing.T, s *Store, rec *notifyRecorder) models.Task {
			task := createRunningTask(t, s, ctx, 99999)
			rec.reset()
			_, err := s.ReconcileGhostTasks(ctx, []int{1})
			must(t, err)
			return *task
		},
	}
}

// The callback runs after commit, so reading the row back from inside it sees
// the new state. Inside the transaction this read would block the writer.
func TestStoreNotifiesAfterCommit(t *testing.T) {
	ctx := context.Background()
	base := newTestStore(t)
	var seen models.TaskState
	var s *Store
	s = base.WithTaskNotifier(func(task models.Task) {
		got, err := s.GetTask(ctx, task.ID)
		if err == nil {
			seen = got.State
		}
	})
	task := seedTestTask(t, s, "after-commit", models.TaskStateRunning)
	if _, err := s.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
		t.Fatal(err)
	}
	if seen != models.TaskStateBlocked {
		t.Fatalf("task state read from the callback = %q, want BLOCKED", seen)
	}
}

// A write that does not commit announces nothing.
func TestStoreDoesNotNotifyFailedWrite(t *testing.T) {
	ctx := context.Background()
	s, rec := notifyingStore(t)
	task := seedTestTask(t, s, "stale-write", models.TaskStateRunning)
	rec.reset()
	stale := task.UpdatedAt.Add(-time.Hour)
	if _, err := s.UpdateTaskState(ctx, task.ID, stale, models.TaskStateBlocked); err == nil {
		t.Fatal("stale write succeeded, want a conflict")
	}
	if len(rec.tasks) != 0 {
		t.Fatalf("notified %+v for a failed write", rec.tasks)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
