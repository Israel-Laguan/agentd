package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestNormalizeDaemonOptions_QueuedReconcileDefaultsToTaskDeadline(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{
		TaskDeadline: 3 * time.Minute,
		Probe:        StaticPIDProbe{},
	})
	if daemon.queuedReconcileAfter != 3*time.Minute {
		t.Fatalf("queuedReconcileAfter = %s, want 3m", daemon.queuedReconcileAfter)
	}
}

func TestReconcileOrphanedQueued_DoesNotResetRecentQueuedClaim(t *testing.T) {
	store := newQueueStore()
	now := time.Now().UTC()
	store.tasks = []models.Task{{
		BaseEntity: models.BaseEntity{ID: "task-0", CreatedAt: now, UpdatedAt: now},
		ProjectID:  "project", AgentID: "default",
		Title: "waiting for worker", State: models.TaskStateQueued, Assignee: models.TaskAssigneeSystem,
	}}

	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		QueuedReconcileAfter: time.Minute,
		Probe:                StaticPIDProbe{},
	})

	if err := daemon.reconcileOrphanedQueued(context.Background()); err != nil {
		t.Fatalf("reconcileOrphanedQueued() error = %v", err)
	}
	task, err := store.GetTask(context.Background(), "task-0")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.State != models.TaskStateQueued {
		t.Fatalf("state = %s, want QUEUED (not yet stale)", task.State)
	}
}

func TestReconcileOrphanedQueued_RecoversStaleClaimWithoutChannelGate(t *testing.T) {
	store := newQueueStore()
	now := time.Now().UTC()
	store.tasks = []models.Task{{
		BaseEntity: models.BaseEntity{ID: "task-0", CreatedAt: now, UpdatedAt: now.Add(-2 * time.Minute)},
		ProjectID:  "project", AgentID: "default",
		Title: "stale claim", State: models.TaskStateQueued, Assignee: models.TaskAssigneeSystem,
	}}

	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		QueuedReconcileAfter: time.Minute,
		Probe:                StaticPIDProbe{},
	})

	if err := daemon.reconcileOrphanedQueued(context.Background()); err != nil {
		t.Fatalf("reconcileOrphanedQueued() error = %v", err)
	}
	task, err := store.GetTask(context.Background(), "task-0")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.State != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", task.State)
	}
}
