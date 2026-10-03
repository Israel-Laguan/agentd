package queue

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
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

// holdGeminiProbe trips the gemini provider breaker, moves past its open timeout
// and takes the single probe slot, leaving it held for the whole test so every
// dispatched sibling is refused with AdmissionProbeInFlight.
func holdGeminiProbe(t *testing.T, pb *ProviderBreakers) *time.Time {
	t.Helper()
	clock := time.Now()
	pb.Get("gemini").SetClockForTest(func() time.Time { return clock })
	for range 3 {
		pb.Get("gemini").RecordError(models.ErrLLMQuotaExceeded)
	}
	clock = clock.Add(6 * time.Minute)
	if got := pb.Get("gemini").Admit(); got != AdmissionProbe {
		t.Fatalf("setup: Admit() = %v, want the probe slot held", got)
	}
	if got := pb.Get("gemini").State(); got != BreakerHalfOpen {
		t.Fatalf("setup: breaker state = %s, want HALF_OPEN with the probe in flight", got)
	}
	return &clock
}

// newSoakDaemon builds a daemon over the fake store with short intervals. The
// global breaker stays CLOSED so dispatch is never gated; the held probe lives on
// the *provider* registry, which is where the sibling wait happens.
func newSoakDaemon(t *testing.T, store *countingStore, pb *ProviderBreakers, taskInterval, maxTaskInterval time.Duration) *Daemon {
	t.Helper()
	gw := &fakeGateway{content: `{"command":"echo ok"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: true}}
	sink := &recordingSink{}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), sink,
		WorkerOptions{ProviderBreakers: pb, MaxRetries: DefaultWorkerMaxRetries})
	return NewDaemon(store, worker, nil, NewCircuitBreaker(), sink, DaemonOptions{
		MaxWorkers: 8, TaskInterval: taskInterval, MaxTaskInterval: maxTaskInterval,
		TaskDeadline: time.Minute, QueuedReconcileAfter: 0, Probe: StaticPIDProbe{},
	})
}

// TestDispatchLoopRequeueRateIsBoundedWhileProbeInFlight (Gap B) pins the rate
// at which siblings cycle while a probe is held.
//
// Nothing enforced this: the behaviour was measured by hand (~1 cycle per
// dispatch tick) and left unpinned, so a future change that requeued in a tight
// loop — a requeue that did not go through the tick, or a tick that stopped
// pacing — would pass every other test while hammering the database once per
// task per iteration. With a probe held the loop's only brake is the tick
// interval itself, so the claim rate is the whole contract.
func TestDispatchLoopRequeueRateIsBoundedWhileProbeInFlight(t *testing.T) {
	const (
		siblings     = 5
		taskInterval = 20 * time.Millisecond
		maxInterval  = 80 * time.Millisecond
		budget       = time.Second
	)

	store := &countingStore{queueStore: newQueueStore()}
	store.profile.Provider = "gemini"
	store.profile.AgenticMode = false
	store.seed(siblings, models.TaskStateReady)

	pb := NewProviderBreakers()
	holdGeminiProbe(t, pb)

	d := newSoakDaemon(t, store, pb, taskInterval, maxInterval)

	claims := soakLoop(t, store, d, budget)
	if len(claims) < 2 {
		t.Fatalf("claims = %d, want at least 2 ticks in %s — the loop never dispatched", len(claims), budget)
	}
	elapsed := claims[len(claims)-1].Sub(claims[0])

	// One claim per tick at most, so the tick count is capped by elapsed /
	// TaskInterval. A hot loop (no pacing at all) would land near elapsed/1ns,
	// which this bound misses by three orders of magnitude; the +2 slack absorbs
	// the first tick and a tick that raced a still-pending async requeue.
	bound := int(elapsed/taskInterval) + 2
	if len(claims) > bound {
		t.Fatalf("dispatch ticks = %d over %s, want <= %d — the loop is not pacing at TaskInterval (%s)",
			len(claims), elapsed, bound, taskInterval)
	}
	// And it must actually have cycled, or the bound is vacuous.
	if len(claims) < 5 {
		t.Fatalf("dispatch ticks = %d over %s, want enough ticks to be meaningful", len(claims), elapsed)
	}

	gaps := consecutiveGaps(claims)
	// Every gap must respect the base interval: the loop waits before each tick.
	for i, g := range gaps {
		if g < taskInterval/2 {
			t.Fatalf("tick %d came %s after the previous one, well under TaskInterval %s — the loop is spinning",
				i, g, taskInterval)
		}
	}
	// A requeued sibling counts as dispatched, so nextDispatchDelay returns the
	// base interval and the tick rate stays at TaskInterval rather than backing
	// off to MaxTaskInterval. That is deliberate: a sibling waiting on a probe is
	// re-polled at the base rate, so the worst-case delay before a recovered
	// provider is noticed is one TaskInterval, not one MaxTaskInterval. The median
	// is used rather than the max so one slow tick on a loaded machine cannot fail
	// this, while a genuine backoff to the cap (4x the base interval) does.
	if med := median(gaps); med > taskInterval*3/2 {
		t.Fatalf("median tick gap = %s, want about TaskInterval (%s) — waiting siblings are being "+
			"backed off toward MaxTaskInterval %s, which delays every sibling behind a slow probe. gaps: %v",
			med, taskInterval, maxInterval, roundGaps(gaps))
	}
	t.Logf("probe-in-flight soak: %d ticks over %s, bound=%d, median gap=%s, max gap=%s, gaps=%v",
		len(claims), elapsed.Round(time.Millisecond), bound,
		median(gaps).Round(time.Millisecond), gaps[len(gaps)-1].Round(time.Millisecond), roundGaps(gaps))
}

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

	// The first gap is the base interval; the last must be the cap. Allow a wide
	// margin for scheduler jitter on a loaded machine but keep it far below the
	// budget, so a loop that never backed off (constant base interval) fails.
	if last := gaps[len(gaps)-1]; last < maxInterval/2 {
		t.Fatalf("final tick gap = %s, want it backed off toward MaxTaskInterval %s (all gaps: %v)",
			last, maxInterval, gaps)
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
