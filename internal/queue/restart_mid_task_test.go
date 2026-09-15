package queue

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
)

// TestBeat1UncleanKillLeavesNoStuckRunning encodes docs/harness-reliability.md Beat 1:
// unclean stop → BootReconcile with dead worker PID → no eternal RUNNING; READY + reboot review.
func TestBeat1UncleanKillLeavesNoStuckRunning(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	taskID := seedInterruptedTask(t, ctx, store)
	sink := &recordingSink{}

	// Probe does not include worker PID 4242 → simulates host without that process after kill -KILL.
	if err := BootReconcile(ctx, store, StaticPIDProbe{PIDs: []int{1}}, sink); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	got, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.State == models.TaskStateRunning {
		t.Fatalf("task state = RUNNING after boot reconcile; Beat 1 forbids silent stuck RUNNING")
	}
	if got.State != models.TaskStateReady {
		t.Fatalf("task state = %s, want READY after ghost recovery", got.State)
	}
	if got.OSProcessID != nil {
		t.Fatalf("os_process_id = %v, want nil after ghost recovery", got.OSProcessID)
	}

	systemID := mustSystemProjectID(t, ctx, store)
	assertRecoveryReviewTask(t, ctx, store, systemID)

	// Explicit board signal (handoff / recovery events), not a silent hang.
	var sawRecovery, sawHandoff bool
	for _, ev := range sink.events {
		if ev.Type == models.EventTypeRecovery && (strings.Contains(ev.Payload, "Recovered Ghost Task") || strings.Contains(ev.Payload, "reset ghost task")) {
			sawRecovery = true
		}
		if ev.Type == RebootRecoveryHandoffEventType {
			sawHandoff = true
		}
	}
	if !sawRecovery || !sawHandoff {
		t.Fatalf("events = %#v, want recovery + reboot handoff signals", sink.events)
	}
}

func TestBeat1AlivePIDRemainsRunning(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	taskID := seedInterruptedTask(t, ctx, store)

	// Worker PID still alive (included in probe) → must not reset.
	if err := BootReconcile(ctx, store, StaticPIDProbe{PIDs: []int{4242}}, nil); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}
	got, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.State != models.TaskStateRunning {
		t.Fatalf("task state = %s, want RUNNING when PID still alive", got.State)
	}
}
