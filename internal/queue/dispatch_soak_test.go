package queue

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// countingStore wraps the shared queueStore fake and records when the dispatch
// loop claims, so a test can measure the real tick cadence of taskLoop.
type countingStore struct {
	*queueStore
	mu    sync.Mutex
	stamp []time.Time
}

func (c *countingStore) ClaimNextReadyTasks(ctx context.Context, limit int) ([]models.Task, error) {
	tasks, err := c.queueStore.ClaimNextReadyTasks(ctx, limit)
	c.mu.Lock()
	c.stamp = append(c.stamp, time.Now())
	c.mu.Unlock()
	return tasks, err
}

func (c *countingStore) claims() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Time(nil), c.stamp...)
}

// soakLoop runs the real taskLoop for budget and returns the claim timestamps.
//
// taskLoop is driven directly (not through Start) so boot reconcile and the
// other seven loops stay out of the measurement. It does `defer d.wg.Done()`,
// so the WaitGroup is primed exactly as Start does.
func soakLoop(t *testing.T, store *countingStore, d *Daemon, budget time.Duration) []time.Time {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	d.wg.Add(1)
	done := make(chan struct{})
	go func() {
		d.taskLoop(ctx)
		close(done)
	}()
	time.Sleep(budget)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("taskLoop did not exit after cancel")
	}
	return store.claims()
}

// newSoakDaemon builds a daemon over the fake store with short intervals. The
// global breaker stays CLOSED so dispatch is never gated. There is no held
// provider probe to keep re-queuing siblings: the dispatch-side provider gate
// left with the builtin exec path in T-070, so this only measures the loop's own
// tick pacing.
func newSoakDaemon(t *testing.T, store *countingStore, pb *ProviderBreakers, taskInterval, maxTaskInterval time.Duration) *Daemon {
	t.Helper()
	gw := &fakeGateway{toolCalls: []gateway.ToolCall{testutil.BashToolCall("call_1", "echo ok")}}
	sb := &fakeSandbox{result: sandbox.Result{Success: true}}
	sink := &recordingSink{}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), sink,
		WorkerOptions{ProviderBreakers: pb, MaxRetries: DefaultWorkerMaxRetries})
	return NewDaemon(store, worker, nil, NewCircuitBreaker(), sink, DaemonOptions{
		MaxWorkers: 8, TaskInterval: taskInterval, MaxTaskInterval: maxTaskInterval,
		TaskDeadline: time.Minute, QueuedReconcileAfter: 0, Probe: StaticPIDProbe{},
	})
}

// The requeue-rate test that used to open this file drove siblings that a held
// provider probe kept re-queuing. The dispatch-side provider gate went with the
// builtin exec path in T-070, so there is no probe for siblings to wait on; the
// backoff half below still pins the tick rate the loop paces itself with.

// TestDispatchLoopBacksOffWhenNothingIsDispatched covers the backoff half.
//
// A sibling requeued behind an in-flight probe is counted as *dispatched*
// (loop_dispatch.go adds the batch size before the worker runs), so nextDispatchDelay
// returns the base interval and that case deliberately does not back off — it is
// rate-bounded by TaskInterval instead, as the test above pins. Backoff is the
// response to the other case: a tick that dispatches nothing. This drives that
// case (an empty queue) and asserts the gap between ticks grows to MaxTaskInterval,
// so a nextDispatchDelay that ignored its inputs could not pass unnoticed.
func TestDispatchLoopBacksOffWhenNothingIsDispatched(t *testing.T) {
	const (
		taskInterval = 20 * time.Millisecond
		maxInterval  = 80 * time.Millisecond
		budget       = time.Second
	)

	store := &countingStore{queueStore: newQueueStore()}
	store.seed(0, models.TaskStateReady)

	d := newSoakDaemon(t, store, NewProviderBreakers(), taskInterval, maxInterval)

	claims := soakLoop(t, store, d, budget)
	if len(claims) < 6 {
		t.Fatalf("claims = %d, want at least 6 ticks in %s to see the ramp", len(claims), budget)
	}
	gaps := consecutiveGaps(claims)

	// The first gap is the base interval; later gaps must be backed off toward
	// the cap. Take the median of the later half rather than the final sample: a
	// single stretched tick would otherwise let a loop stuck on the base
	// interval pass. Allow a wide margin for scheduler jitter on a loaded
	// machine but keep it far below the budget.
	if med := median(gaps[len(gaps)/2:]); med < maxInterval/2 {
		t.Fatalf("median of the later tick gaps = %s, want it backed off toward MaxTaskInterval %s (all gaps: %v)",
			med, maxInterval, gaps)
	}
	// No gap may exceed the cap plus slack, or the loop stopped dispatching.
	if last := gaps[len(gaps)-1]; last > maxInterval*3 {
		t.Fatalf("final tick gap = %s, far beyond MaxTaskInterval %s — the loop stalled", last, maxInterval)
	}
	t.Logf("empty-queue backoff: %d ticks, gaps=%v", len(claims), roundGaps(gaps))
}

func consecutiveGaps(stamps []time.Time) []time.Duration {
	if len(stamps) < 2 {
		return nil
	}
	gaps := make([]time.Duration, 0, len(stamps)-1)
	for i := 1; i < len(stamps); i++ {
		gaps = append(gaps, stamps[i].Sub(stamps[i-1]))
	}
	return gaps
}

func median(gaps []time.Duration) time.Duration {
	if len(gaps) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), gaps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

func roundGaps(gaps []time.Duration) []time.Duration {
	out := make([]time.Duration, len(gaps))
	for i, g := range gaps {
		out[i] = g.Round(time.Millisecond)
	}
	return out
}
