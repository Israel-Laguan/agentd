package queue

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// B-018: the dispatch loop's own cleanup path. requeueClaimedTask reads the row
// and then writes it with the version it read; a writer committing in between
// used to leave the claimed task QUEUED with only a log line.
func TestRequeueClaimedTaskSurvivesLostOptimisticLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := testutil.NewFakeStore()
	if _, _, err := fake.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "requeue-claimed",
		Tasks:       []models.DraftTask{{Title: "t", Description: "d"}},
	}); err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	claimed, err := fake.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimNextReadyTasks() = (%d tasks, %v)", len(claimed), err)
	}
	store := testutil.NewContendedStateStore(fake, 1)
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{MaxWorkers: 1, Probe: StaticPIDProbe{}})

	daemon.requeueClaimedTask(ctx, claimed[0])

	task, err := fake.GetTask(ctx, claimed[0].ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.State != models.TaskStateReady {
		t.Fatalf("state = %s, want READY: the requeue lost the lock and was dropped", task.State)
	}
}
