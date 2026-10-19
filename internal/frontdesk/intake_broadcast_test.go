package frontdesk

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

type broadcastSink struct{ broadcast []models.Event }

func (b *broadcastSink) Emit(context.Context, models.Event) error { return nil }
func (b *broadcastSink) Broadcast(_ context.Context, ev models.Event) error {
	b.broadcast = append(b.broadcast, ev)
	return nil
}

// releaseParent changes task states with the store directly; live subscribers
// must hear about each changed task or the board shows them stale (T-047).
func TestIntakeProcessorBroadcastsReleasedTasks(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d", Tasks: []models.DraftTask{{Title: "parent"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	parent := driveToState(t, ctx, store, tasks[0], true)
	ref := commentAndListRef(t, ctx, store, parent.ID, "more work")
	gw := &stubIntakeGateway{structDraft: models.DraftPlan{Tasks: []models.DraftTask{{Title: "one"}, {Title: "two"}}}}
	sink := &broadcastSink{}

	if err := NewIntakeProcessor(store, gw, sink, nil, 0).Process(ctx, ref); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(sink.broadcast) != 2 {
		t.Fatalf("broadcasts = %d, want one per released follow-up (2)", len(sink.broadcast))
	}
	for _, ev := range sink.broadcast {
		if ev.Type != models.EventTypeTaskUpdated || !ev.TaskID.Valid || ev.ProjectID == "" {
			t.Fatalf("broadcast = %+v, want TASK_UPDATED naming task and project", ev)
		}
	}
}
