//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/agent/acp/fakeagent"

	acpsdk "github.com/coder/acp-go-sdk"
)

const acpBaseURL = "http://localhost:8773"

// TestJ24_ACPAgentRunsTaskInJail tests J24: with exec.turn routed to an acp
// agent, a task runs on it in the bubblewrap jail inside the container and is
// committed from its report_outcome. The agent is the devenv build's scripted
// fake (T-069), so no model runs the task. An acp agent is refused without a
// usable jail (sandbox.backend: bubblewrap in config.acp.yaml), so COMPLETED
// also proves the jail ran.
func TestJ24_ACPAgentRunsTaskInJail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	harness := NewHarness(acpBaseURL, "acp")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J24 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(acpBaseURL, harness.client)

	const summary = "J24: the fake acp agent finished in the jail"
	script := fakeagent.Script(
		fakeagent.Step{Did: acpsdk.ToolKindEdit},
		fakeagent.Step{Tool: &fakeagent.Tool{Name: "report_outcome", Args: map[string]any{
			"outcome": "completed", "summary": summary, "result": map[string]any{"summary": summary},
		}}},
	)
	materialized := materializePlan(ctx, t, client, "J24", DraftPlan{
		ProjectName:         UniqueProjectName("j24"),
		StartEmptyWorkspace: true,
		Tasks: []DraftTask{{
			Title:           "J24 acp task",
			Description:     script,
			SuccessCriteria: []string{"the acp agent reports the task completed"},
		}},
	})
	task := materialized.Tasks[0]

	poller := NewTaskPoller(client, materialized.Project.ID)
	if got, err := poller.WaitForTaskState(ctx, task.ID, TaskStateCompleted, 60*time.Second); err != nil {
		t.Fatalf("J24 [run] task did not complete on the acp agent: %v (last observed: %+v)", err, got)
	}
	events, err := client.ListTaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("J24 [events] %v", err)
	}
	for _, ev := range events {
		if ev.Type == "RESULT" && strings.Contains(ev.Payload, summary) && strings.Contains(ev.Payload, `"outcome":"completed"`) {
			return
		}
	}
	t.Fatalf("J24 [result] no RESULT event carries a completed outcome with the agent's summary %q; events: %+v", summary, events)
}
