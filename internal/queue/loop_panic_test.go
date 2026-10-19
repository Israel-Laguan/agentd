package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"agentd/internal/models"
)

// panicOnceStore panics on the first reconcile call of each kind and counts
// every call, so a test can tell whether the loop survived to tick again.
type panicOnceStore struct {
	*queueStore
	hitlCalls, orphanCalls atomic.Int32
}

func (s *panicOnceStore) ReconcileExpiredBlockedTasks(ctx context.Context, now time.Time) ([]models.Task, error) {
	if s.hitlCalls.Add(1) == 1 {
		panic("boom in hitl reconcile")
	}
	return nil, nil
}

func (s *panicOnceStore) ReconcileOrphanedQueued(ctx context.Context, minAge time.Duration) ([]models.Task, error) {
	if s.orphanCalls.Add(1) == 1 {
		panic("boom in orphan reconcile")
	}
	return nil, nil
}

// T-050: a panic inside a background loop tick is recovered and logged; the
// loop keeps ticking instead of taking the whole daemon down.
func TestBackgroundLoopsSurviveTickPanic(t *testing.T) {
	store := &panicOnceStore{queueStore: newQueueStore()}
	d := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		Probe: StaticPIDProbe{}, HITLReconcileEvery: 5 * time.Millisecond,
		QueuedReconcileAfter: 20 * time.Millisecond, TaskInterval: time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	d.wg.Add(2)
	go d.hitlTimeoutLoop(ctx)
	go d.queuedReconcileLoop(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for (store.hitlCalls.Load() < 2 || store.orphanCalls.Load() < 2) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	d.wg.Wait()
	if h, o := store.hitlCalls.Load(), store.orphanCalls.Load(); h < 2 || o < 2 {
		t.Fatalf("loops did not tick again after a panic: hitl=%d orphan=%d", h, o)
	}
}
