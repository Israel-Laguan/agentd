//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"
)

// j09OutageModel is the mock model behind the breaker profile's `flaky`
// provider (devenv/agentd/config.breaker.yaml). The journey owns it: it is down
// for the trip half and brought back for the recovery half.
const j09OutageModel = "brk-outage"

// j09BreakerOpenTimeout mirrors breaker.open_timeout in config.breaker.yaml.
const j09BreakerOpenTimeout = 10 * time.Second

// j09RecoveryBound is how long recovery may take after the provider is back: the
// open timeout, one probe, and slack for the claim tick. It is deliberately far
// below the 5 minute default, so a timeout that stopped being read from config
// fails here instead of passing slowly.
const j09RecoveryBound = j09BreakerOpenTimeout + 45*time.Second

// j09SetOutage switches the flaky provider's outage and registers a cleanup that
// brings it back, so a failed run never leaves the shared mock down.
func j09SetOutage(ctx context.Context, t *testing.T, down bool) {
	t.Helper()

	mock := NewMockLLMClient(mockLLMBaseURL)
	if err := mock.SetOutage(ctx, j09OutageModel, down); err != nil {
		t.Fatalf("J09 [outage] set down=%v: %v", down, err)
	}
	if down {
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = mock.SetOutage(cleanupCtx, j09OutageModel, false)
		})
	}
}

// j09AwaitRecovery ends the outage and proves the daemon resumes by itself: the
// breaker returns to CLOSED and a task that was waiting behind the trip reaches
// COMPLETED, with no call to the breaker reset endpoint. It fails if recovery
// takes longer than j09RecoveryBound.
func j09AwaitRecovery(ctx context.Context, t *testing.T, client *APIClient, projectID string) time.Duration {
	t.Helper()

	j09SetOutage(ctx, t, false)
	start := time.Now()
	poller := NewTaskPoller(client, projectID)

	var lastState string
	var completed int
	for time.Since(start) < j09RecoveryBound {
		if status := j09ReadStatus(ctx, t, client); status.Breaker != nil {
			lastState = string(status.Breaker.State)
		}
		completed = 0
		if tasks, err := poller.listTasks(ctx, false); err == nil {
			for _, task := range tasks {
				if task.State == TaskStateCompleted {
					completed++
				}
			}
		}
		if lastState == string(BreakerClosed) && completed > 0 {
			return time.Since(start)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("J09 [recovery] after the outage ended, breaker %s and %d completed task(s) in project %s "+
		"within %s — the breaker did not recover on its own (latched HALF_OPEN, or breaker.open_timeout is not read)",
		lastState, completed, projectID, j09RecoveryBound)
	return 0
}
