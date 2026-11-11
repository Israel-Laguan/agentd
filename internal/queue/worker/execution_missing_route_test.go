package worker

import (
	"context"
	"os"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestReadyTaskWithoutExecTurnRouteFailsToHuman(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(t.TempDir())
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "missing-route", Tasks: []models.DraftTask{{Title: "Explain the project", Description: testutil.AgenticTestTaskDescription()}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project.WorkspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	gw := &sequenceGateway{responses: []gateway.AIResponse{{Content: "The builtin executed this task."}}}
	w := NewWorker(store, gw, &mockAgenticSandbox{}, nil, &mockEventSink{}, WorkerOptions{})
	w.Process(ctx, tasks[0])
	got := mustGetTask(t, store, tasks[0].ID)
	if got.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state = %s, want FAILED_REQUIRES_HUMAN for missing exec.turn route", got.State)
	}
	events, err := store.ListEventsByTask(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(event.Payload, "exec.turn") {
			return
		}
	}
	t.Fatalf("events = %+v; want the missing exec.turn route named", events)
}

// An unclassified task (the plan omitted the field) keeps write access: only a
// task the plan explicitly classified as text-only is read-only.
func TestUnclassifiedTextTaskRunsOnWritableACPWorkspace(t *testing.T) {
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	w, store, task, project, profile, _, log := acpFixture(t, script, false)
	task.RequiresWorkspaceChangeClassified = false
	w.dispatchProcess(context.Background(), task, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", got)
	}
	if got := log.all(); len(got) != 1 || got[0] {
		t.Fatalf("ACP read-only arguments = %v, want [false] for an unclassified task", got)
	}
}

func TestTextTaskRunsOnReadOnlyACPWorkspace(t *testing.T) {
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	w, store, task, project, profile, _, log := acpFixture(t, script, false)
	w.dispatchProcess(context.Background(), task, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", got)
	}
	if got := log.all(); len(got) != 1 || !got[0] {
		t.Fatalf("ACP read-only arguments = %v, want [true] for text task", got)
	}
}
