//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// j16Tasks builds n independent tasks. They must not depend on each other: the
// siblings only overlap if the dispatcher can claim them at the same time.
func j16Tasks(tag string, n int) []DraftTask {
	tasks := make([]DraftTask, 0, n)
	for i := 1; i <= n; i++ {
		tasks = append(tasks, DraftTask{
			Title:       fmt.Sprintf("J16 %s task %d", tag, i),
			Description: "Echo the task id into PLAN_RESULTS.log.",
		})
	}
	return tasks
}

func j16TaskStates(tasks []Task) string {
	out := ""
	for _, task := range tasks {
		out += fmt.Sprintf("[%s %s] ", task.Title, task.State)
	}
	return out
}

// j16ResetBreaker resets every breaker through the real endpoint, so the journey
// starts from CLOSED for both the global and the per-provider registry.
func j16ResetBreaker(ctx context.Context, t *testing.T, client *APIClient) {
	t.Helper()

	resp, err := client.ResetBreaker(ctx)
	if err != nil {
		t.Fatalf("J16 [breaker reset] request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("J16 [breaker reset] returned %d, want 200", resp.StatusCode)
	}
}

// j16SetAgentProvider points the default profile at this profile's only provider.
// The worker's provider gate is a no-op for an empty profile provider
// (worker_provider_gate.go's admitProvider), so without this no provider breaker
// is ever consulted.
func j16SetAgentProvider(ctx context.Context, t *testing.T, client *APIClient) {
	t.Helper()

	resp, err := client.SetAgentProviderModel(ctx, "default", j16Provider, j16Model, false)
	if err != nil {
		t.Fatalf("J16 [agent] patch default profile failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("J16 [agent] patch default profile returned %d, want 200", resp.StatusCode)
	}
}

// j16SetQuota arms or disarms the 429 response for this journey's model, and
// registers a cleanup that disarms it, so a failed run never leaves the shared
// mock 429ing for another journey.
func j16SetQuota(ctx context.Context, t *testing.T, mock *MockLLMClient, on bool) {
	t.Helper()

	if err := mock.SetQuota(ctx, j16Model, on); err != nil {
		t.Fatalf("J16 [mock] set quota on=%v for %s: %v", on, j16Model, err)
	}
	if on {
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = mock.SetQuota(cleanupCtx, j16Model, false)
		})
	}
}

func j16SetSlowOnce(ctx context.Context, t *testing.T, mock *MockLLMClient, d time.Duration) {
	t.Helper()

	if err := mock.SetSlowOnce(ctx, j16Model, d.Seconds()); err != nil {
		t.Fatalf("J16 [mock] arm slow_once %s for %s: %v", d, j16Model, err)
	}
}

// j16AwaitProviderBreaker polls system/status for the journey's provider entry.
// provider_breakers is the only surface that exposes a per-provider breaker.
func j16AwaitProviderBreaker(ctx context.Context, t *testing.T, client *APIClient, want, why string) {
	t.Helper()

	deadline := time.Now().Add(j16TripBound)
	last := "absent from provider_breakers"
	for time.Now().Before(deadline) {
		if entry, ok := j16ProviderBreaker(ctx, t, client); ok {
			last = fmt.Sprintf("%s (%d failures)", entry.State, entry.FailureCount)
			if entry.State == want {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("J16 [breaker] provider %s was %s, want %s (%s)", j16Provider, last, want, why)
}

func j16ProviderBreaker(ctx context.Context, t *testing.T, client *APIClient) (ProviderBreaker, bool) {
	t.Helper()

	resp, err := client.SystemStatus(ctx)
	if err != nil {
		t.Fatalf("J16 [system/status] request failed: %v", err)
	}
	status, err := DecodeSystemStatus(resp)
	if err != nil {
		t.Fatalf("J16 [system/status] decode failed: %v", err)
	}
	entry, ok := status.ProviderBreakers[j16Provider]
	return entry, ok
}

// j16AssertArmingHandoff confirms the trip really was the quota path: the arming
// tasks must have been handed off, which is what a quota verdict does and an
// unreachable verdict does not.
func j16AssertArmingHandoff(ctx context.Context, t *testing.T, client *APIClient, projectID string) {
	t.Helper()

	deadline := time.Now().Add(j16TripBound)
	for time.Now().Before(deadline) {
		if task, err := NewTaskPoller(client, projectID).PollHumanHandoff(ctx); err == nil && task != nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("J16 [arm] no HUMAN handoff in project %s — the quota failures did not take the provider branch", projectID)
}

// j16AssertNoHandoffWhileProbeInFlight is the journey's core assertion: for as
// long as the probe is in flight, no sibling may be blocked on a human. It fails
// on the first violation rather than at the end of the window, so a handoff that
// happened early is still caught even if the probe later resolves.
//
// Both halves are checked because they are produced by different code: the HUMAN
// subtask by createProviderExhaustedHandoff, the event by the Emit beside it.
func j16AssertNoHandoffWhileProbeInFlight(ctx context.Context, t *testing.T, client *APIClient, projectID string) {
	t.Helper()

	poller := NewTaskPoller(client, projectID)
	deadline := time.Now().Add(j16ObserveWindow)
	for time.Now().Before(deadline) {
		if handoff, err := poller.PollHumanHandoff(ctx); err == nil && handoff != nil {
			t.Fatalf("J16 [probe] task %s was handed to a human while the provider's probe was still in flight", handoff.ID)
		}
		for _, task := range j16ListTasks(ctx, t, client, projectID) {
			for _, event := range j16TaskEvents(ctx, t, client, task.ID) {
				if event.Type == "PROVIDER_EXHAUSTED_HANDOFF" {
					t.Fatalf("J16 [probe] task %s emitted PROVIDER_EXHAUSTED_HANDOFF while the probe was in flight: %s",
						task.ID, event.Payload)
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// j16ListTasks lists every task in the project including the healing handoffs,
// because the HUMAN children this journey asserts the absence of are filtered out
// of a default listing.
func j16ListTasks(ctx context.Context, t *testing.T, client *APIClient, projectID string) []Task {
	t.Helper()

	tasks, err := NewTaskPoller(client, projectID).listTasks(ctx, true)
	if err != nil {
		t.Fatalf("J16 [tasks] list failed: %v", err)
	}
	return tasks
}

func j16TaskEvents(ctx context.Context, t *testing.T, client *APIClient, taskID string) []TaskEvent {
	t.Helper()

	events, err := client.ListTaskEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("J16 [events] list for task %s failed: %v", taskID, err)
	}
	return events
}
