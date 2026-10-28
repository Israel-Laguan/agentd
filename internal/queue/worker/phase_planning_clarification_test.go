package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

const phaseClarification = `{"project_name":"P","clarification_question":"What should the next phase change?"}`

// The next-phase intent is machine-built, so a question has nobody to answer
// it. It must not complete the phase task with zero tasks and lose the question.
func TestPhasePlanningQuestionDoesNotCompleteThePhaseTask(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{{Title: "Plan Phase 2"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	agent := &stageRecorder{output: phaseClarification}
	sink := &mockEventSink{}
	w := NewWorker(store, nil, nil, nil, sink, WorkerOptions{})
	w.stageAgent = agent

	before, _ := store.ListTasksByProject(ctx, project.ID)
	w.handlePhasePlanning(ctx, tasks[0], *project)
	after, _ := store.ListTasksByProject(ctx, project.ID)

	if len(after) != len(before) {
		t.Fatalf("tasks = %d, want %d: a question adds none", len(after), len(before))
	}
	got, err := store.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State == models.TaskStateCompleted {
		t.Fatalf("phase task is %s, want it not completed with 0 tasks", got.State)
	}
	if !anyEventMentions(sink, "What should the next phase change?") {
		t.Fatalf("events = %+v, want the model's question kept in an event", sink.events)
	}
}

func anyEventMentions(sink *mockEventSink, want string) bool {
	for _, ev := range sink.events {
		if strings.Contains(ev.Payload, want) {
			return true
		}
	}
	return false
}
