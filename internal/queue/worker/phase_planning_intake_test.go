package worker

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/testutil"
)

const threeNextPhaseTasks = `{"project_name":"P","tasks":[` +
	`{"title":"a","description":"d","assignee":"SYSTEM"},` +
	`{"title":"b","description":"d","assignee":"SYSTEM"},` +
	`{"title":"c","description":"d","assignee":"SYSTEM"}]}`

func TestPhasePlanningRunsTheTaskCreationStageWithItsPurposeAndCap(t *testing.T) {
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
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{MaxTasksPerPhase: 2})
	w.stageAgent = agent

	before, err := store.ListTasksByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.handlePhasePlanning(ctx, tasks[0], *project)
	after, err := store.ListTasksByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(agent.reqs) != 1 {
		t.Fatalf("intake calls = %d, want 1", len(agent.reqs))
	}
	req := agent.reqs[0]
	if req.Stage != stage.StageIntake || req.Purpose != gateway.PurposeCreateNextPhase {
		t.Fatalf("call = stage %q purpose %q, want intake and next phase", req.Stage, req.Purpose)
	}
	if agent.taskIDs[0] != tasks[0].ID {
		t.Fatalf("plan TaskID = %q, want %q", agent.taskIDs[0], tasks[0].ID)
	}
	if got := len(after) - len(before); got != 2 {
		t.Fatalf("created %d tasks, want 2 (the phase cap)", got)
	}
}
