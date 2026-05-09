package bus

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestEventBridgeNilEmitter(t *testing.T) {
	b := EventBridge{}
	if err := b.Emit(context.Background(), models.Event{}); err != nil {
		t.Fatalf("emit: %v", err)
	}
}

func TestEventBridgeDelegatesToEmitter(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	proj, _, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "bus", Tasks: []models.DraftTask{{Title: "t"}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	b := NewInProcess()
	em := NewEventEmitter(store, b)
	bridge := EventBridge{Emitter: em}
	if err := bridge.Emit(ctx, models.Event{
		BaseEntity: models.BaseEntity{ID: "evt-1"},
		ProjectID:  proj.ID,
		Type:       models.EventTypeComment,
		Payload:    "hello",
	}); err != nil {
		t.Fatalf("emit: %v", err)
	}
}
