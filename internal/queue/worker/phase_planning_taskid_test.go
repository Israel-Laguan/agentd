package worker

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// T-057: next-phase planning runs for the "Plan Phase N" task, so its plan
// call is attributed to that task.
func TestPhasePlanningCallCarriesTheTaskID(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{{Title: "Plan Phase 2"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	agent := &stageRecorder{output: threeNextPhaseTasks}
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{})
	w.stageAgent = agent

	w.handlePhasePlanning(ctx, tasks[0], *project)

	if len(agent.taskIDs) != 1 || agent.taskIDs[0] != tasks[0].ID {
		t.Fatalf("plan TaskID = %v, want %q", agent.taskIDs, tasks[0].ID)
	}
}
