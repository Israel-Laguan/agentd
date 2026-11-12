package queue

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// A failed attempt takes the tuner's next step. The step the coordinator can
// still apply is the requeue and the TUNE event: the model override itself went
// with the builtin loop (docs/harness-extraction.md, "What agentd loses").
func TestWorkerTuneStepRequeuesAndEmitsTune(t *testing.T) {
	store := newACPWorkerStore(t)
	store.task.RetryCount = 1
	scriptTask(store, fakeagent.Step{Error: "tool crashed"})
	sink := &recordingSink{}
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepLowerTemperature, HealingStepLowerTemperature},
	})
	worker := newACPWorker(t, store, &fakeGateway{}, &fakeSandbox{}, sink, WorkerOptions{Tuner: tuner})

	worker.Process(context.Background(), store.task)

	if store.task.RetryCount != 2 || store.task.State != models.TaskStateReady {
		t.Fatalf("retries=%d state=%s", store.task.RetryCount, store.task.State)
	}
	if !sink.hasEvent("TUNE") {
		t.Fatalf("events = %#v, want TUNE", sink.events)
	}
}

func TestWorkerHealingSplitBlocksTaskWithSubtasks(t *testing.T) {
	store := newACPWorkerStore(t)
	scriptTask(store, fakeagent.Step{Error: "too complex for one turn"})
	gw := &fakeGateway{content: `{"subtasks":[{"title":"Smaller","description":"Do smaller"}]}`}
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepSplitTask},
	})
	worker := newACPWorker(t, store, gw, &fakeSandbox{}, nil, WorkerOptions{Tuner: tuner})

	worker.Process(context.Background(), store.task)

	if store.task.State != models.TaskStateBlocked || len(store.drafts) != 1 || store.drafts[0].Title != "Smaller" {
		t.Fatalf("state=%s drafts=%#v", store.task.State, store.drafts)
	}
}

func TestWorkerHealingHumanHandoff(t *testing.T) {
	store := newACPWorkerStore(t)
	scriptTask(store, fakeagent.Step{Error: "tool crashed"})
	tuner := NewParameterTuner(config.HealingConfig{
		Enabled: true,
		Steps:   []string{HealingStepHumanHandoff},
	})
	worker := newACPWorker(t, store, &fakeGateway{}, &fakeSandbox{}, nil, WorkerOptions{Tuner: tuner})

	worker.Process(context.Background(), store.task)

	if store.task.State != models.TaskStateBlocked || len(store.drafts) != 1 {
		t.Fatalf("state=%s drafts=%#v", store.task.State, store.drafts)
	}
	if store.drafts[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("assignee = %s", store.drafts[0].Assignee)
	}
}

func TestWorkerPhasePlanningTaskAppendsNextPhase(t *testing.T) {
	store := newWorkerStore()
	store.task.Title = "Plan Phase 2"
	store.task.Description = "Continue with API and UI work."
	store.project.Name = "Roadmap"
	store.project.OriginalInput = "Build the roadmap"
	store.tasks = []models.Task{
		{BaseEntity: models.BaseEntity{ID: "done"}, Title: "Initial setup", State: models.TaskStateCompleted},
		store.task,
	}
	gw := &fakeGateway{plan: &models.DraftPlan{ProjectName: "Roadmap", Tasks: []models.DraftTask{{Title: "Build API"}, {Title: "Plan Phase 2"}}}}
	sb := &fakeSandbox{result: sandbox.Result{Success: true}}
	sink := &recordingSink{}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), sink, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if gw.planCalls != 1 {
		t.Fatalf("GeneratePlan calls = %d, want 1", gw.planCalls)
	}
	if len(gw.requests) != 1 || gw.requests[0].Purpose != gateway.PurposeCreateNextPhase {
		t.Fatalf("Generate requests = %d, want the one next-phase intake call", len(gw.requests))
	}
	if len(sb.commands) != 0 {
		t.Fatalf("sandbox commands = %#v, want none", sb.commands)
	}
	if store.appends != 1 || len(store.drafts) != 2 || store.drafts[0].Title != "Build API" || store.drafts[1].Title != "Plan Phase 3" {
		t.Fatalf("appends=%d drafts=%#v", store.appends, store.drafts)
	}
	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v, want success", store.result)
	}
	if gw.planPurpose != gateway.PurposeCreateNextPhase {
		t.Fatalf("plan purpose = %q, want %q", gw.planPurpose, gateway.PurposeCreateNextPhase)
	}
	if !strings.Contains(gw.lastPlanIntent, "Plan Phase 3") {
		t.Fatalf("last plan intent missing next phase: %q", gw.lastPlanIntent)
	}
	if !sink.hasEvent("PHASE_PLANNING") {
		t.Fatalf("events = %#v, want PHASE_PLANNING", sink.events)
	}
}
