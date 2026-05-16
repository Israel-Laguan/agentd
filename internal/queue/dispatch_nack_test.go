package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
)

func TestDispatch_RateLimitedTaskStaysQueued(t *testing.T) {
	store := newQueueStore()
	store.seed(1, models.TaskStateReady)

	gate := NewChannelGate(config.ChannelConfig{
		MaxMessageSize: 1024,
		RateLimit:      1,
		RateWindow:     60,
	})
	task, err := store.GetTask(context.Background(), "task-0")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if r := gate.Admit(TaskToInbound(*task)); r.Disposition != Ack {
		t.Fatalf("pre-admit should ack: %v", r.Err)
	}

	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		MaxWorkers:           1,
		TaskInterval:         time.Hour,
		Channel:              gate,
		QueuedReconcileAfter: time.Hour,
		Probe:                StaticPIDProbe{},
	})

	ctx := context.Background()
	claimed, err := daemon.dispatch(ctx)
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if claimed != 0 {
		t.Fatalf("claimed = %d, want 0", claimed)
	}
	after, err := store.GetTask(ctx, "task-0")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if after.State != models.TaskStateQueued {
		t.Fatalf("state = %s, want QUEUED", after.State)
	}

	claimed2, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	if len(claimed2) != 0 {
		t.Fatalf("re-claimed %d tasks, want 0 while deferred", len(claimed2))
	}
}

func TestDispatch_InvalidTaskMarkedFailed(t *testing.T) {
	store := newQueueStore()
	now := time.Now().UTC()
	store.tasks = []models.Task{{
		BaseEntity: models.BaseEntity{ID: "task-0", CreatedAt: now, UpdatedAt: now},
		ProjectID:  "project", AgentID: "default",
		Title: "", Description: "", State: models.TaskStateReady, Assignee: models.TaskAssigneeSystem,
	}}

	gate := NewChannelGate(config.ChannelConfig{MaxMessageSize: 1024})
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		MaxWorkers: 1, TaskInterval: time.Hour, Channel: gate, Probe: StaticPIDProbe{},
	})

	claimed, err := daemon.dispatch(context.Background())
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if claimed != 0 {
		t.Fatalf("claimed = %d, want 0", claimed)
	}
	task, err := store.GetTask(context.Background(), "task-0")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.State != models.TaskStateFailed {
		t.Fatalf("state = %s, want FAILED", task.State)
	}

	for range 5 {
		claimed, err := store.ClaimNextReadyTasks(context.Background(), 1)
		if err != nil {
			t.Fatalf("ClaimNextReadyTasks() error = %v", err)
		}
		if len(claimed) != 0 {
			t.Fatal("failed task was re-claimed")
		}
	}
}

func TestDispatch_EarlyReturnSubtractsNacked(t *testing.T) {
	store := newQueueStore()
	store.seed(2, models.TaskStateReady)

	gate := NewChannelGate(config.ChannelConfig{
		MaxMessageSize: 1024,
		RateLimit:      1,
		RateWindow:     60,
	})
	task0, err := store.GetTask(context.Background(), "task-0")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if r := gate.Admit(TaskToInbound(*task0)); r.Disposition != Ack {
		t.Fatalf("pre-admit task-0: %v", r.Err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		MaxWorkers: 2, TaskInterval: time.Hour, Channel: gate, Probe: StaticPIDProbe{},
	})

	claimed, err := daemon.dispatch(ctx)
	if claimed != 1 {
		t.Fatalf("claimed = %d, want 1 (2 tasks - 1 nacked), err=%v", claimed, err)
	}
}
