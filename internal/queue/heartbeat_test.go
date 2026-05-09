package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestHeartbeatLoopReconcilesStaleTasks(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	running := createQueueRunningTask(t, store, ctx, 4242)
	time.Sleep(time.Millisecond)
	sink := &recordingSink{}
	daemon := NewDaemon(store, nil, nil, nil, sink, DaemonOptions{
		MaxWorkers: 1,
		Probe:      StaticPIDProbe{PIDs: []int{4242}},
		StaleAfter: time.Nanosecond,
	})

	if err := daemon.reconcileHeartbeats(ctx); err != nil {
		t.Fatalf("reconcileHeartbeats() error = %v", err)
	}
	task, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.State != models.TaskStateReady || task.OSProcessID != nil {
		t.Fatalf("task = %#v, want READY without pid", task)
	}
	if len(sink.events) != 1 || sink.events[0].Type != HeartbeatReconcileEventType {
		t.Fatalf("events = %#v, want HEARTBEAT_RECONCILE", sink.events)
	}
}

func TestHeartbeatLoopSkipsHealthyTasks(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	running := createQueueRunningTask(t, store, ctx, 4242)
	sink := &recordingSink{}
	daemon := NewDaemon(store, nil, nil, nil, sink, DaemonOptions{
		MaxWorkers: 1,
		Probe:      StaticPIDProbe{PIDs: []int{4242}},
		StaleAfter: time.Hour,
	})

	if err := daemon.reconcileHeartbeats(ctx); err != nil {
		t.Fatalf("reconcileHeartbeats() error = %v", err)
	}
	task, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.State != models.TaskStateRunning || task.OSProcessID == nil {
		t.Fatalf("task = %#v, want RUNNING with pid", task)
	}
	if len(sink.events) != 0 {
		t.Fatalf("events = %#v, want none", sink.events)
	}
}

func createQueueRunningTask(t *testing.T, store interface {
	MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error)
	ClaimNextReadyTasks(context.Context, int) ([]models.Task, error)
	MarkTaskRunning(context.Context, string, time.Time, int) (*models.Task, error)
}, ctx context.Context, pid int) *models.Task {
	t.Helper()
	_, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "heartbeat",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, pid)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}
	return running
}
