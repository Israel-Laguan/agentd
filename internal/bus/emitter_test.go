package bus_test

import (
	"context"
	"database/sql"
	"testing"

	"agentd/internal/bus"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestEventEmitterEmit(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()
	project, tasks := materializeEmitterPlan(t, store)
	eventBus := bus.NewInProcess()
	ch, unsubscribe := eventBus.Subscribe("task:"+tasks[0].ID, 1)
	defer unsubscribe()
	globalCh, unsubscribeGlobal := eventBus.Subscribe(bus.GlobalTopic, 1)
	defer unsubscribeGlobal()

	emitter := bus.NewEventEmitter(store, eventBus)
	err := emitter.Emit(ctx, models.Event{
		ProjectID: project.ID,
		TaskID:    sql.NullString{String: tasks[0].ID, Valid: true},
		Type:      "LOG",
		Payload:   "sandbox output",
	})
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	if got := len(store.Events()); got != 1 {
		t.Fatalf("events count = %d, want 1", got)
	}
	got := <-ch
	if got.Topic != "task:"+tasks[0].ID || got.Type != "LOG" || got.Payload != "sandbox output" {
		t.Fatalf("bus event = %#v", got)
	}
	global := <-globalCh
	if global.Topic != bus.GlobalTopic || global.Type != "LOG" {
		t.Fatalf("global event = %#v", global)
	}
}

func materializeEmitterPlan(t *testing.T, store *testutil.FakeKanbanStore) (*models.Project, []models.Task) {
	t.Helper()
	project, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "emitter",
		Description: "test emitter",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	return project, tasks
}
