//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestJ12_TieredExecution tests J12: tiered execution with escalation on
// verify failure.
//
// The tiered profile (agentd-tiered on :8769, tiered.enabled: true) splits a
// complex task into a context/decision/execute/verify DAG. The mock LLM drives
// each step (devenv/mockllm/server.py detects the step from its system-prompt
// suffix) and fails the verify step, so the escalation ladder runs: bounded
// mid-fix redos, then a strong-model escalate that completes the origin.
//
// Pass criteria (docs/testing/journeys.md J12): the small model is tried
// first, the verify failure triggers escalation, and the final step uses the
// full model — observed through the origin reaching COMPLETED and the mock's
// request capture showing both a verify-step and an escalate-step request.
func TestJ12_TieredExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	harness := NewHarness(tieredBaseURL, "tiered")
	if err := harness.WaitForHealthy(ctx, 15*time.Second); err != nil {
		t.Fatalf("J12 [boot] tiered profile failed to become healthy on %s: %v", tieredBaseURL, err)
	}
	client := NewAPIClient(tieredBaseURL, harness.client)

	// A single root task whose title+description scores at or above the tiered
	// complexity threshold (default 200), so the worker splits it into the
	// tiered DAG. start_empty_workspace unlocks it immediately.
	materialized := materializePlan(ctx, t, client, "J12", DraftPlan{
		ProjectName:         UniqueProjectName("j12"),
		Description:         "J12: tiered execution with verify failure and escalation",
		StartEmptyWorkspace: true,
		Tasks: []DraftTask{{
			Title:       "J12 tiered pipeline",
			Description: strings.Repeat("a", 220), // 9 + 220 = 229 >= 200
		}},
	})
	originID := materialized.Tasks[0].ID

	// The origin is BLOCKED while its steps run, then resolves to COMPLETED
	// via the escalation ladder (verify always fails, so only a successful
	// escalate completes it).
	poller := NewTaskPoller(client, materialized.Project.ID)
	finalTasks, err := poller.WaitForAllComplete(ctx, 240*time.Second)
	if err != nil {
		t.Fatalf("J12 [run] tiered pipeline did not complete: %v (last observed: %+v)", err, finalTasks)
	}
	origin := findTask(finalTasks, originID)
	if origin == nil {
		t.Fatalf("J12 [run] origin task %s missing from final task list", originID)
	}
	if origin.State != TaskStateCompleted {
		t.Fatalf("J12 [run] origin state = %q, want COMPLETED (escalation should have resolved it)", origin.State)
	}

	// The mock's request capture is the observation channel for which steps ran:
	// the verify step must have run (and failed) and the escalate step must have
	// run after it. Filter captured requests by this run's task IDs so prior
	// append-only capture entries cannot satisfy the checks.
	mock := NewMockLLMClient(mockLLMBaseURL)
	requests, err := mock.Requests(ctx)
	if err != nil {
		t.Fatalf("J12 [mock capture] %v", err)
	}
	ids := make(map[string]struct{}, len(finalTasks))
	for _, task := range finalTasks {
		ids[task.ID] = struct{}{}
	}
	var runRequests []capturedRequest
	for _, request := range requests {
		if _, ok := ids[request.taskID()]; ok {
			runRequests = append(runRequests, request)
		}
	}
	requests = runRequests
	if !mockHasStep(requests, "TIERED MODE: VERIFY STEP") {
		t.Fatal("J12 [mock capture] no verify-step request found — the verify step never ran")
	}
	if !mockHasStep(requests, "TIERED MODE: ESCALATE STEP") {
		t.Fatal("J12 [mock capture] no escalate-step request found — verify failure did not trigger escalation")
	}
	t.Logf("J12: origin %s COMPLETED via escalation; verify and escalate steps both observed", originID)
}

// findTask returns the task with the given ID, or nil.
func findTask(tasks []Task, id string) *Task {
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i]
		}
	}
	return nil
}

// mockHasStep reports whether any captured request's prompt contains the
// tiered step marker.
func mockHasStep(requests []capturedRequest, marker string) bool {
	for _, r := range requests {
		if strings.Contains(r.prompt(), marker) {
			return true
		}
	}
	return false
}
