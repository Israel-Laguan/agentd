//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// j11Preference is the preference saved and then expected in a later task's
// prompt. The token is unique per run so a stale capture entry from an
// earlier run can never satisfy the assertion.
func j11PreferenceText() string {
	return fmt.Sprintf("J11 PREF CANARY %d always answer in haiku", time.Now().UnixNano())
}

// TestJ11_PreferenceRecallOnLaterTask tests J11: a preference saved through
// POST /api/v1/preferences is recalled and shown to the agent on a LATER
// task's execution prompt.
//
// The journey is deliberately two-phase, which is what makes it a recall test
// rather than a plumbing test:
//
//	Phase 1 materializes and runs a project for a user BEFORE the preference
//	exists. Its captured prompt must NOT contain the canary — proving the
//	preference was not already in context.
//	Phase 2 saves the preference, then materializes a second project for the
//	same user. Its captured prompt MUST contain the canary, without the
//	client ever re-sending it.
//
// Observability: nothing in the agentd API exposes prompt contents, so the
// devenv mock LLM appends every request body it receives to a JSONL log and
// serves it back at GET /requests (see devenv/mockllm/server.py's
// record_request). Each request carries agentd_metadata.task_id, which is how
// a captured prompt is attributed to the task that produced it.
func TestJ11_PreferenceRecallOnLaterTask(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 15*time.Second); err != nil {
		t.Fatalf("J11 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)

	// The breaker is process-global and may have been left OPEN by a prior test
	// (e.g. J09). Reset it to prevent dispatch from being blocked for 5 minutes.
	j11ResetBreaker(ctx, t, client)

	mock := NewMockLLMClient(mockLLMBaseURL)

	const userID = "j11-user"
	canary := j11PreferenceText()

	// Phase 1: baseline. The preference does not exist yet, so the worker's
	// prompt for this task must not contain it. If it did, the assertion in
	// phase 2 would be meaningless.
	beforeTask := j11RunProject(ctx, t, client, mock, userID, "j11-before", "J11 baseline before the preference exists")
	j11AssertPrefAbsent(t, "phase 1 (preference not yet saved)", beforeTask, canary)

	// Save the preference.
	j11SavePreference(ctx, t, client, userID, canary)

	// Phase 2: a different project, materialized after the save. The
	// preference must now reach the worker's prompt unprompted.
	afterTask := j11RunProject(ctx, t, client, mock, userID, "j11-after", "J11 task after the preference was saved")
	j11AssertPrefPresent(t, "phase 2 (later task)", afterTask, canary)

	// A different user must NOT see it: recall is scoped by user_id, not
	// global. Without this, a bug that leaked every preference into every
	// prompt would still pass the phase-2 assertion above.
	otherTask := j11RunProject(ctx, t, client, mock, "j11-other-user", "j11-other", "J11 task for an unrelated user")
	j11AssertPrefAbsent(t, "cross-user isolation", otherTask, canary)

	t.Logf("J11: preference %q absent before save, present in task %s after, absent for an unrelated user",
		canary, afterTask.taskID)
}

// j11CapturedTask is one task's execution prompt, as captured by the mock.
type j11CapturedTask struct {
	taskID   string
	prompts  []string
	requests int
}

// j11RunProject materializes a single-task project, waits for it to complete,
// and returns the prompt(s) the mock received for that task's execution.
func j11RunProject(
	ctx context.Context,
	t *testing.T,
	client *APIClient,
	mock *MockLLMClient,
	userID, tag, description string,
) j11CapturedTask {
	t.Helper()

	projectName := UniqueProjectName(tag)
	plan := DraftPlan{
		ProjectName:         projectName,
		Description:         description,
		StartEmptyWorkspace: true,
		Tasks: []DraftTask{
			{Title: fmt.Sprintf("%s worker task", tag), Description: description},
		},
	}

	// The requesting identity rides on the header, matching the chat
	// endpoint's X-Agentd-User convention; the controller stamps it onto the
	// project, and the worker reads it back when building the prompt.
	resp, err := client.MaterializePlanForUser(ctx, plan, userID)
	if err != nil {
		t.Fatalf("%s [materialize] request failed: %v", tag, err)
	}
	if resp.StatusCode != http.StatusCreated {
		_ = resp.Body.Close()
		t.Fatalf("%s [materialize] returned %d, want 201", tag, resp.StatusCode)
	}
	materialized, err := DecodeMaterializeResult(resp)
	if err != nil {
		t.Fatalf("%s [materialize] decode failed: %v", tag, err)
	}
	projectID, taskID := materialized.Project.ID, materialized.Tasks[0].ID

	poller := NewTaskPoller(client, projectID)
	if _, err := poller.WaitForAllComplete(ctx, 90*time.Second); err != nil {
		t.Fatalf("%s [tasks] did not complete: %v", tag, err)
	}

	captured := j11CapturedTask{taskID: taskID}
	prompts, err := mock.WorkerPrompts(ctx, taskID)
	if err != nil {
		t.Fatalf("%s [mock] could not read captured requests: %v", tag, err)
	}
	captured.prompts = prompts
	captured.requests = len(prompts)
	if len(prompts) == 0 {
		t.Fatalf("%s [mock] the mock LLM captured no request for task %s — the capture log "+
			"is unreachable (is mockllm publishing :8000?)", tag, taskID)
	}
	return captured
}

func j11AssertPrefPresent(t *testing.T, phase string, captured j11CapturedTask, canary string) {
	t.Helper()

	for _, prompt := range captured.prompts {
		if strings.Contains(prompt, canary) {
			return
		}
	}
	t.Fatalf("J11 [%s] preference %q did not reach the prompt for task %s (%d request(s) captured)\n"+
		"prompts seen:\n%s", phase, canary, captured.taskID, captured.requests, j11Indent(captured.prompts))
}

func j11AssertPrefAbsent(t *testing.T, phase string, captured j11CapturedTask, canary string) {
	t.Helper()

	for i, prompt := range captured.prompts {
		if strings.Contains(prompt, canary) {
			t.Fatalf("J11 [%s] preference %q appeared in request %d for task %s, but should not have — "+
				"recall is not scoped to the requesting user", phase, canary, i, captured.taskID)
		}
	}
}

func j11Indent(prompts []string) string {
	var b strings.Builder
	for i, p := range prompts {
		fmt.Fprintf(&b, "  --- request %d ---\n%s\n", i, p)
	}
	return b.String()
}

func j11SavePreference(ctx context.Context, t *testing.T, client *APIClient, userID, text string) {
	t.Helper()

	resp, err := client.SavePreference(ctx, userID, text)
	if err != nil {
		t.Fatalf("J11 [preferences] request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("J11 [preferences] returned %d, want 201", resp.StatusCode)
	}
}

// j11ResetBreaker resets the process-global circuit breaker before the
// journey starts. The breaker is not per-project: a prior run (e.g. J09)
// may have left it OPEN, which throttles new task dispatch for up to
// safety.DefaultBreakerTimeout (5 minutes) before it's even attempted.
// Resetting first keeps the journey fast and repeatable regardless of
// what ran before it.
func j11ResetBreaker(ctx context.Context, t *testing.T, client *APIClient) {
	t.Helper()

	resp, err := client.ResetBreaker(ctx)
	if err != nil {
		t.Fatalf("J11 [breaker reset] request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J11 [breaker reset] returned %d, want 200", resp.StatusCode)
	}
}
