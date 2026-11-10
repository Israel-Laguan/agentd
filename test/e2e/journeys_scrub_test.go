//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"
)

// j17Token matches the default profile's sandbox.scrub_patterns entry
// ("E2E-SCRUB-[0-9]{6}", devenv/agentd/config.yaml) and nothing else.
const j17Token = "E2E-SCRUB-482913"

// TestJ17_EventsAPIScrubsSecrets tests J17: a value matching
// sandbox.scrub_patterns never leaves the daemon through the task events API.
//
// The fake agent's report_outcome summary carries the token, so it reaches the
// RESULT event. The API must serve it with "[REDACTED]" in its place (B-065:
// the pattern used to apply to the sandbox only, not to the events the API
// serves). The ACP path writes no COMMAND or LOG_CHUNK events, so RESULT is
// the event under test.
func TestJ17_EventsAPIScrubsSecrets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J17 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)
	plan := DraftPlan{
		ProjectName:         UniqueProjectName("j17"),
		Description:         "J17 events scrub task",
		StartEmptyWorkspace: true,
		Tasks:               []DraftTask{fakeTask("J17 scrub", "J17 agent summary carries "+j17Token)},
	}
	projectID := materializePlan(ctx, t, client, "J17", plan).Project.ID
	tasks, err := NewTaskPoller(client, projectID).WaitForAllComplete(ctx, 30*time.Second)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("J17 [run] task did not complete: %v (last observed: %+v)", err, tasks)
	}

	events, err := client.ListTaskEvents(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("J17 [events] %v", err)
	}
	redacted := 0
	for _, e := range events {
		if strings.Contains(e.Payload, j17Token) {
			t.Errorf("J17 [scrub] %s event served the raw token: %q", e.Type, e.Payload)
		}
		if strings.Contains(e.Payload, "[REDACTED]") {
			redacted++
		}
	}
	if redacted == 0 {
		t.Fatalf("J17 [scrub] no event carries [REDACTED]; did the token reach the events? %+v", events)
	}
	t.Logf("J17: %d of %d events served with the token redacted", redacted, len(events))
}
