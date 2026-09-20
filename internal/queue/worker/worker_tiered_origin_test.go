package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// TestCompleteTieredOrigin_AtomicFromBlocked (SP-006): a BLOCKED origin
// resolves straight to COMPLETED in one store call. Atomicity comes from
// that single write (no intermediate READY is ever persisted), not from any
// post-hoc claim check.
func TestCompleteTieredOrigin_AtomicFromBlocked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}

	origin := newTieredPipelineFixture(t, store, "origin-atomic-complete")
	w.completeTieredOrigin(ctx, origin, "tiered pipeline verified")

	current, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if current.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", current.State)
	}
	var resolved bool
	for _, ev := range sink.events {
		if ev.Type == "TIERED_PIPELINE_RESOLVED" && strings.Contains(ev.Payload, "success=true") {
			resolved = true
		}
	}
	if !resolved {
		t.Fatalf("no TIERED_PIPELINE_RESOLVED success event, got %+v", sink.events)
	}
	events, err := store.ListEventsByTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask: %v", err)
	}
	var resultEvent bool
	for _, ev := range events {
		if ev.Type == models.EventTypeResult && ev.Payload == "tiered pipeline verified" {
			resultEvent = true
		}
	}
	if !resultEvent {
		t.Fatal("no persisted RESULT event with the pipeline verdict")
	}
}

// TestFailTieredOrigin_AtomicFromBlocked: the failure path resolves through
// the same single call — BLOCKED straight to FAILED.
func TestFailTieredOrigin_AtomicFromBlocked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}

	origin := newTieredPipelineFixture(t, store, "origin-atomic-fail")
	w.failTieredOrigin(ctx, origin, "Tiered step failed; pipeline cancelled")

	current, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if current.State != models.TaskStateFailed {
		t.Fatalf("state = %s, want FAILED", current.State)
	}
	var resolved bool
	for _, ev := range sink.events {
		if ev.Type == "TIERED_PIPELINE_RESOLVED" && strings.Contains(ev.Payload, "success=false") {
			resolved = true
		}
	}
	if !resolved {
		t.Fatalf("no TIERED_PIPELINE_RESOLVED failure event, got %+v", sink.events)
	}
}

// raceWinningOriginStore simulates the SP-006 interleaving deterministically:
// a concurrent dispatcher moves the origin between our re-read and our
// completion write, so the version we complete with is stale and the store
// reports ErrStateConflict.
type raceWinningOriginStore struct {
	*testutil.FakeKanbanStore
}

func (s *raceWinningOriginStore) CompleteTieredOrigin(ctx context.Context, id string, expected time.Time, result models.TaskResult) (*models.Task, error) {
	current, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.IncrementRetryCount(ctx, id, current.UpdatedAt); err != nil {
		return nil, err
	}
	return s.FakeKanbanStore.CompleteTieredOrigin(ctx, id, expected, result)
}

// TestCompleteTieredOrigin_StaleVersionLeavesOrigin: when another writer
// moved the origin first (the stale-dispatcher race from SP-006), the
// version check conflicts and the origin is left alone — no verdict is
// forced onto fresh state, and no resolution event is emitted.
func TestCompleteTieredOrigin_StaleVersionLeavesOrigin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &raceWinningOriginStore{testutil.NewFakeStore()}
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}

	origin := newTieredPipelineFixture(t, store.FakeKanbanStore, "origin-atomic-stale")

	w.completeTieredOrigin(ctx, origin, "tiered pipeline verified")

	current, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if current.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want still BLOCKED (stale verdict must not land)", current.State)
	}
	for _, ev := range sink.events {
		if ev.Type == "TIERED_PIPELINE_RESOLVED" {
			t.Fatalf("unexpected TIERED_PIPELINE_RESOLVED event on conflict: %+v", sink.events)
		}
	}
}
