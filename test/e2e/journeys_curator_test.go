//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"
)

const eventsPurgedType = "EVENTS_PURGED"

// TestJ18_CuratorCuratesEachTaskOnce tests J18: the memory curator curates and
// purges a completed task once, and later passes leave it alone (B-066).
//
// The disk profile runs the curator every 5s (devenv/agentd/crontab.disk) with
// retention 0 hours and archive grace 0 days (config.disk.yaml), so the first
// pass after the task completes archives it, records a memory and purges its
// events, leaving one EVENTS_PURGED marker. Before B-066 the marker made the
// task eligible again: each later pass re-curated it and the purge replaced
// the marker. So the journey pins the marker's identity, not only the count:
// the same single EVENTS_PURGED event, and nothing else, after three more
// passes.
func TestJ18_CuratorCuratesEachTaskOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(diskBaseURL, "disk")
	if err := harness.WaitForHealthy(ctx, 30*time.Second); err != nil {
		t.Fatalf("J18 [boot] disk profile not healthy on %s: %v", diskBaseURL, err)
	}
	client := NewAPIClient(diskBaseURL, harness.client)
	plan := DraftPlan{
		ProjectName:         UniqueProjectName("j18"),
		Description:         "J18 curator idempotency task",
		StartEmptyWorkspace: true,
		Tasks:               []DraftTask{{Title: "J18 curator check", Description: "Complete so the curator archives this task once."}},
	}
	projectID := materializePlan(ctx, t, client, "J18", plan).Project.ID
	tasks, err := NewTaskPoller(client, projectID).WaitForAllComplete(ctx, 30*time.Second)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("J18 [run] task did not complete: %v (last observed: %+v)", err, tasks)
	}
	taskID := tasks[0].ID

	marker := waitForPurgeMarker(ctx, t, client, taskID, 30*time.Second)
	// Three more curator passes at 5s each, plus slack.
	time.Sleep(20 * time.Second)
	events, err := client.ListTaskEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("J18 [events] %v", err)
	}
	if len(events) != 1 || events[0].ID != marker.ID {
		t.Fatalf("J18 [once] after three more passes the task has %d events, want only the first purge marker %s: %+v",
			len(events), marker.ID, events)
	}
	t.Logf("J18: task %s curated once; purge marker %s unchanged across later passes", taskID, marker.ID)
}

// waitForPurgeMarker polls until the task's events are exactly one
// EVENTS_PURGED marker, the state the curator's first pass leaves.
func waitForPurgeMarker(ctx context.Context, t *testing.T, client *APIClient, taskID string, timeout time.Duration) TaskEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var events []TaskEvent
	for time.Now().Before(deadline) {
		var err error
		events, err = client.ListTaskEvents(ctx, taskID)
		if err != nil {
			t.Fatalf("J18 [events] %v", err)
		}
		if len(events) == 1 && events[0].Type == eventsPurgedType {
			return events[0]
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("J18 [purge] task %s was not curated and purged within %v; events: %+v", taskID, timeout, events)
	return TaskEvent{}
}
