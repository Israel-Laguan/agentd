package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// SP-012: how long until work resumes after a provider outage ends?
//
// The spike question had three parts, and the premise in the spike file was
// that they needed to be discovered. Two of them are answered here by reading
// the dispatch path, one required measurement:
//
//   - The breaker holds OPEN for DefaultBreakerTimeout, which is a package
//     constant (safety.defaultBreakerTimeout), not a config key. The
//     `breaker.handoff_after` config key is the *handoff* delay, a different
//     knob, so an operator cannot shorten the outage pause at all.
//   - Tasks that failed during the outage are not uniformly retryable. The two
//     that failed before the trip are requeued to READY with their retry count
//     untouched; the one that trips the breaker is blocked and handed to HUMAN.
//   - A HALF_OPEN probe is spent before the daemon knows whether it has a task
//     to run, so a queue that happens to be empty at the timeout latches the
//     breaker HALF_OPEN forever. This is measured, not proven here: the
//     reproduction is in spikes/SP-012-provider-outage-recovery.md and the fix
//     is sized as T-035, out of scope for this spike.

// TestOutageRecoveryFailedTasksSplitBetweenRequeueAndHandoff pins the two
// different fates an outage-failed task gets, because "are failed tasks retried
// automatically" has no single answer.
func TestOutageRecoveryFailedTasksSplitBetweenRequeueAndHandoff(t *testing.T) {
	store := newQueueStore()
	gateway := &queueGateway{content: `{"command":"true"}`, err: models.ErrLLMUnreachable}
	sb := &queueSandbox{result: sandbox.Result{Success: true, ExitCode: 0}}
	breaker := NewCircuitBreaker()
	now := time.Now().UTC()
	breaker.SetClockForTest(func() time.Time { return now })
	sink := &queueSink{}
	worker := NewWorker(store, gateway, sb, breaker, sink, WorkerOptions{MaxRetries: DefaultWorkerMaxRetries})
	daemon := NewDaemon(store, worker, nil, breaker, sink, DaemonOptions{MaxWorkers: 3, TaskInterval: time.Hour})

	store.seed(3, models.TaskStateReady)

	ctx := context.Background()
	if _, _, err := daemon.dispatch(ctx); err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if err := waitFor(func() bool { return daemon.sem.InUse() == 0 }, "outage workers finished"); err != nil {
		t.Fatalf("workers did not finish: %v", err)
	}
	if breaker.State() != BreakerOpen {
		t.Fatalf("breaker state = %s, want OPEN", breaker.State())
	}

	// The two that failed before the trip went back to READY with no retry
	// counted: an outage is not the task's fault.
	if got := store.count(models.TaskStateReady); got != 2 {
		t.Fatalf("READY tasks = %d, want 2 (pre-trip failures requeued)", got)
	}
	// The one that tripped the breaker is blocked for a human.
	if got := store.count(models.TaskStateBlocked); got != 1 {
		t.Fatalf("BLOCKED tasks = %d, want 1 (trip task handed off)", got)
	}
	for _, task := range outageStoreTasks(store) {
		if task.RetryCount != 0 {
			t.Fatalf("task %s RetryCount = %d, want 0 — an outage must not consume the retry budget",
				task.ID, task.RetryCount)
		}
	}
	if !sink.containsType("PROVIDER_EXHAUSTED_HANDOFF") {
		t.Fatal("no PROVIDER_EXHAUSTED_HANDOFF event, want the trip task handed to a human")
	}
}

func outageStoreTasks(s *queueStore) []models.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Task(nil), s.tasks...)
}

// TestOutageRecoveryTimeToResume pins the outage pause and shows it is a
// constant rather than a tunable, which is the operator-facing answer to "how
// long until work resumes".
func TestOutageRecoveryTimeToResume(t *testing.T) {
	breaker := NewCircuitBreaker()
	now := time.Now().UTC()
	breaker.SetClockForTest(func() time.Time { return now })

	for range 3 {
		breaker.RecordError(models.ErrLLMUnreachable)
	}
	if breaker.State() != BreakerOpen {
		t.Fatalf("breaker state = %s, want OPEN", breaker.State())
	}

	// Still blocking just before the timeout.
	now = now.Add(DefaultBreakerTimeout - time.Second)
	if got := breaker.ProbeLimit(1); got != 0 {
		t.Fatalf("ProbeLimit just before the timeout = %d, want 0 — the pause is the whole %s", got, DefaultBreakerTimeout)
	}

	// At the timeout it admits exactly one probe, not the whole queue.
	now = now.Add(time.Second)
	if got := breaker.ProbeLimit(4); got != 1 {
		t.Fatalf("ProbeLimit at the timeout = %d, want 1 (single probe)", got)
	}

	breaker.RecordSuccess()
	if breaker.State() != BreakerClosed {
		t.Fatalf("breaker state after the probe succeeded = %s, want CLOSED", breaker.State())
	}
	if got := breaker.ProbeLimit(4); got != 4 {
		t.Fatalf("ProbeLimit once CLOSED = %d, want 4 — normal polling resumes", got)
	}
}

// TestOutageRecoveryFailedProbeReopensTheBreaker shows the outage pause is not a
// single 5-minute wait: a probe that fails during a flapping outage restarts the
// full timeout, so a provider that recovers and immediately drops again holds
// work for 5 minutes per attempt.
func TestOutageRecoveryFailedProbeReopensTheBreaker(t *testing.T) {
	breaker := NewCircuitBreaker()
	now := time.Now().UTC()
	breaker.SetClockForTest(func() time.Time { return now })

	for range 3 {
		breaker.RecordError(models.ErrLLMUnreachable)
	}
	now = now.Add(DefaultBreakerTimeout + time.Second)
	if got := breaker.ProbeLimit(1); got != 1 {
		t.Fatalf("ProbeLimit = %d, want 1", got)
	}
	breaker.RecordError(models.ErrLLMUnreachable)

	if breaker.State() != BreakerOpen {
		t.Fatalf("breaker state after a failed probe = %s, want OPEN", breaker.State())
	}
	// Still blocking: the clock restarted from the failed probe, not from the trip.
	now = now.Add(DefaultBreakerTimeout - time.Second)
	if got := breaker.ProbeLimit(1); got != 0 {
		t.Fatalf("ProbeLimit %s after the failed probe = %d, want 0 — the timeout restarts",
			DefaultBreakerTimeout-time.Second, got)
	}
}
