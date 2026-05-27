package worker

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/testutil"
)

func TestHandleGatewayError_HealingDisabled_FailsTask(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink, healingEnabled: false}

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "heal-disabled",
		Tasks:       []models.DraftTask{{Title: "task-1", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]

	w.handleGatewayError(context.Background(), task, models.ErrLLMQuotaExceeded)

	children, _ := store.ListChildTasks(context.Background(), task.ID)
	if len(children) > 0 {
		t.Fatalf("expected no child tasks when healing disabled, got %d", len(children))
	}
	updated, err := store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.State != models.TaskStateFailed {
		t.Fatalf("state = %s, want FAILED", updated.State)
	}

	var found bool
	for _, ev := range sink.events {
		if ev.Type == "PROVIDER_EXHAUSTED_HANDOFF" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected PROVIDER_EXHAUSTED_HANDOFF event even when healing disabled")
	}
}

func TestHandleGatewayError_HealingEnabled_CreatesHandoff(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink, healingEnabled: true}

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "heal-enabled",
		Tasks:       []models.DraftTask{{Title: "task-1", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]

	w.handleGatewayError(context.Background(), task, models.ErrLLMQuotaExceeded)

	children, _ := store.ListChildTasks(context.Background(), task.ID)
	humanCount := 0
	for _, c := range children {
		if c.Assignee == models.TaskAssigneeHuman {
			humanCount++
		}
	}
	if humanCount == 0 {
		t.Fatal("expected HUMAN child task when healing enabled")
	}
}

func TestHealingTaskCapReached_NoLimit(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	w := &Worker{store: store, maxHealingTasks: 0}

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "cap-none",
		Tasks:       []models.DraftTask{{Title: "task-1", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if w.healingTaskCapReached(context.Background(), tasks[0]) {
		t.Fatal("should not be capped when maxHealingTasks = 0")
	}
}

func TestHealingTaskCapReached_LimitReached(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink, healingEnabled: true, maxHealingTasks: 2}

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "cap-test",
		Tasks:       []models.DraftTask{{Title: "task-1", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]

	// Create 2 handoffs
	w.createProviderExhaustedHandoff(context.Background(), task, models.ErrLLMQuotaExceeded)
	// Refresh task to get the updated version
	fresh, _ := store.GetTask(context.Background(), task.ID)
	if fresh != nil {
		task = *fresh
	}
	w.createProviderExhaustedHandoff(context.Background(), task, models.ErrLLMQuotaExceeded)
	fresh, _ = store.GetTask(context.Background(), task.ID)
	if fresh != nil {
		task = *fresh
	}

	if !w.healingTaskCapReached(context.Background(), task) {
		t.Fatal("cap should be reached after 2 HUMAN child tasks")
	}
}

func TestCreateHealingHandoff_CapReached_FailsTask(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink, healingEnabled: true, maxHealingTasks: 1}

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "cap-handoff",
		Tasks:       []models.DraftTask{{Title: "task-1", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]

	// First handoff should succeed
	w.createProviderExhaustedHandoff(context.Background(), task, models.ErrLLMQuotaExceeded)

	fresh, _ := store.GetTask(context.Background(), task.ID)
	if fresh != nil {
		task = *fresh
	}

	// Second handoff via healing should be capped
	action := planning.HealingAction{
		Type:     planning.HealingActionHuman,
		StepName: planning.HealingStepHumanHandoff,
		Reason:   "test cap",
	}
	w.createHealingHandoff(context.Background(), task, action, "test payload")

	// Should have emitted a cap-reached event
	var capEvent bool
	for _, ev := range sink.events {
		if ev.Type == "HEALING_HANDOFF" && ev.Payload != "" {
			if len(ev.Payload) > 0 {
				capEvent = true
			}
		}
	}
	if !capEvent {
		t.Fatal("expected HEALING_HANDOFF event after cap reached")
	}
}
