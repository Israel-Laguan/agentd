package queue

import (
	"testing"
	"time"

	"agentd/internal/models"
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

// The two fates an outage-failed task used to get are no longer reachable from
// exec.turn: an unreachable provider was an error the builtin loop got from
// agentd's own gateway call, and after T-070 the task runs on an external agent
// that agentd does not call. The breaker's own behaviour is pinned below and in
// outage_trip_handoff_test.go; the recovery handoff is pinned in the feature
// suite (features/outage_handoff.feature).

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

	// Once the restarted timeout expires, probes are admitted again.
	now = now.Add(time.Second)
	if got := breaker.ProbeLimit(1); got != 1 {
		t.Fatalf("ProbeLimit at the restarted timeout = %d, want 1", got)
	}
}

func outageStoreTasks(s *queueStore) []models.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Task(nil), s.tasks...)
}
