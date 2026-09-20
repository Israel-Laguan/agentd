package kanban

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

// seedTieredOrigin inserts a task and parks it BLOCKED, the state a real
// tiered pipeline origin sits in for the whole DAG's duration.
func seedTieredOrigin(t *testing.T, store *Store, ctx context.Context, title string) models.Task {
	t.Helper()
	seedProfile(t, store, ctx, "default")
	running := seedTestTask(t, store, title, models.TaskStateRunning)
	blocked, err := store.UpdateTaskState(ctx, running.ID, running.UpdatedAt, models.TaskStateBlocked)
	if err != nil {
		t.Fatalf("UpdateTaskState(BLOCKED) error = %v", err)
	}
	return *blocked
}

// TestCompleteTieredOrigin_AtomicFromBlocked is the SP-006 core case: a
// BLOCKED origin resolves straight to COMPLETED in one call, and no
// claimable READY state ever exists for another dispatcher to grab —
// ClaimNextReadyTasks finds nothing both before (BLOCKED) and after
// (COMPLETED) the call.
func TestCompleteTieredOrigin_AtomicFromBlocked(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	origin := seedTieredOrigin(t, store, ctx, "tiered-origin-blocked")

	if claimed, err := store.ClaimNextReadyTasks(ctx, 10); err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	} else if len(claimed) != 0 {
		t.Fatalf("claimed %d tasks before completion, want 0 (origin is BLOCKED)", len(claimed))
	}

	done, err := store.CompleteTieredOrigin(ctx, origin.ID, origin.UpdatedAt, models.TaskResult{Success: true, Payload: "tiered pipeline verified"})
	if err != nil {
		t.Fatalf("CompleteTieredOrigin() error = %v", err)
	}
	if done.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", done.State)
	}
	if done.CompletedAt == nil {
		t.Fatal("CompletedAt is nil, want set")
	}

	if claimed, err := store.ClaimNextReadyTasks(ctx, 10); err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	} else if len(claimed) != 0 {
		t.Fatalf("claimed %d tasks after completion, want 0 (no transient READY leaked)", len(claimed))
	}

	events, err := store.ListEventsByTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask() error = %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Type == models.EventTypeResult && ev.Payload == "tiered pipeline verified" {
			found = true
		}
	}
	if !found {
		t.Fatal("no RESULT event with the pipeline verdict")
	}
}

// TestCompleteTieredOrigin_StaleVersionConflicts proves the atomic write
// keeps its optimistic-concurrency guard: replaying the same version after
// the origin already resolved reports ErrStateConflict instead of
// double-completing — the stale-dispatcher case from SP-006.
func TestCompleteTieredOrigin_StaleVersionConflicts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	origin := seedTieredOrigin(t, store, ctx, "tiered-origin-stale")

	if _, err := store.CompleteTieredOrigin(ctx, origin.ID, origin.UpdatedAt, models.TaskResult{Success: true, Payload: "first"}); err != nil {
		t.Fatalf("first CompleteTieredOrigin() error = %v", err)
	}
	if _, err := store.CompleteTieredOrigin(ctx, origin.ID, origin.UpdatedAt, models.TaskResult{Success: true, Payload: "stale replay"}); !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("replay error = %v, want ErrStateConflict", err)
	}
}

// TestCompleteTieredOrigin_FromReadyAndRunning covers the other two legal
// source states: an origin the store already unblocked to READY, and one
// still RUNNING, both resolve without a ladder walk.
func TestCompleteTieredOrigin_FromReadyAndRunning(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	ready := seedTieredOrigin(t, store, ctx, "tiered-origin-ready")
	unblocked, err := store.UpdateTaskState(ctx, ready.ID, ready.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState(READY) error = %v", err)
	}
	if done, err := store.CompleteTieredOrigin(ctx, unblocked.ID, unblocked.UpdatedAt, models.TaskResult{Success: true, Payload: "ok"}); err != nil {
		t.Fatalf("CompleteTieredOrigin(READY) error = %v", err)
	} else if done.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", done.State)
	}

	seeded := seedTieredOrigin(t, store, ctx, "tiered-origin-fail")
	unblockedRun, err := store.UpdateTaskState(ctx, seeded.ID, seeded.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState(READY) error = %v", err)
	}
	running, err := store.UpdateTaskState(ctx, unblockedRun.ID, unblockedRun.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("UpdateTaskState(RUNNING) error = %v", err)
	}
	if failed, err := store.CompleteTieredOrigin(ctx, running.ID, running.UpdatedAt, models.TaskResult{Success: false, Payload: "pipeline cancelled"}); err != nil {
		t.Fatalf("CompleteTieredOrigin(RUNNING, fail) error = %v", err)
	} else if failed.State != models.TaskStateFailed {
		t.Fatalf("state = %s, want FAILED", failed.State)
	}
}
