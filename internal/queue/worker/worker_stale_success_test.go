package worker

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// B-015, production reach: commitSucceeded (worker_retry.go) records a success on
// the global breaker when a task's sandbox command succeeds. That command can run
// for minutes after the LLM call that produced it, so a task that was already
// executing when the breaker tripped finishes into an OPEN breaker. Its success
// says nothing about the provider now and must not close the outage.
func TestCommitSucceededDoesNotCloseABreakerThatTrippedWhileTheCommandRan(t *testing.T) {
	store := testutil.NewFakeStore()
	breaker := safety.NewCircuitBreaker()
	w := &Worker{store: store, sink: &broadcastSink{}, breaker: breaker}
	task := seedCommitTask(t, store)
	for i := 0; i < safety.DefaultBreakerFailures; i++ {
		breaker.RecordError(models.ErrLLMUnreachable)
	}
	if breaker.State() != safety.BreakerOpen {
		t.Fatalf("setup: breaker = %s, want OPEN", breaker.State())
	}

	if ok := w.commitSucceeded(context.Background(), task, sandbox.Result{Success: true}, nil); !ok {
		t.Fatal("commitSucceeded() = false, want the task's own result committed")
	}

	if got := breaker.State(); got != safety.BreakerOpen {
		t.Fatalf("breaker = %s, want OPEN: a command that finished after the trip closed a live outage", got)
	}
}
