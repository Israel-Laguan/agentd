package worker

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// phasePlanTaskGateway records the task on the ctx of the plan call.
type phasePlanTaskGateway struct {
	subagentMockGateway
	planTask string
}

func (g *phasePlanTaskGateway) GeneratePlan(ctx context.Context, _ string) (*models.DraftPlan, error) {
	g.planTask = gateway.TaskIDFromContext(ctx)
	return nil, nil
}

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
	gw := &phasePlanTaskGateway{}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{})

	w.handlePhasePlanning(ctx, tasks[0], *project)

	if gw.planTask != tasks[0].ID {
		t.Fatalf("plan TaskID = %q, want %q", gw.planTask, tasks[0].ID)
	}
}
