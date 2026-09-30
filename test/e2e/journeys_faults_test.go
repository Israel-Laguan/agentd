//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestJ09_ProviderCascade tests J09 part A: a dead primary provider cascades
// to a live secondary.
//
// The faults profile (agentd-faults on :8767) configures gateway.order as
// [dead, secondary] — see devenv/agentd/config.faults.yaml. "dead" points at
// http://localhost:9999/v1, where nothing listens, so the router's cascade
// (internal/gateway/routing/router_cascade.go's generateOnce) must fail over
// to "secondary" (litellm → mockllm) and still return a usable completion.
//
// There is no HTTP surface for the router's internal AIResponse.ProviderUsed
// field, and no response header carrying it, so the assertion is behavioural:
// the request SUCCEEDS against a profile whose first-choice provider cannot
// possibly answer. A regression that stopped cascading would surface as a
// models.ErrLLMUnreachable failure here rather than a silent fallback.
//
// The differential half of the journey is TestJ09_BreakerOpens, which runs
// against a profile where every provider is dead — so a success here is
// attributable to the cascade, not to a lenient gateway.
// hitlSubtaskTitleManualReview mirrors internal/models.HITLSubtaskTitleManualReview.
// Spelled out rather than imported because the e2e package intentionally does
// not import internal/ — test/e2e/client.go duplicates the wire shapes for the
// same reason.
const hitlSubtaskTitleManualReview = "Manual review required:"

// TestJ09_ProviderCascade tests J09 part A: a dead primary provider cascades
// to a live secondary.
func TestJ09_ProviderCascade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	harness := NewHarness(faultsBaseURL, "faults")
	if err := harness.WaitForHealthy(ctx, 15*time.Second); err != nil {
		t.Fatalf("J09 [boot] faults profile not healthy on %s: %v", faultsBaseURL, err)
	}
	client := NewAPIClient(faultsBaseURL, harness.client)

	// A status_check intent is answered by StatusSummarizer from the store,
	// so the only LLM call on this path is the intent classification itself —
	// exactly one gateway round trip, which is what the cascade covers.
	resp, err := client.ChatCompletions(ctx, "What is the current status of things?", nil)
	if err != nil {
		t.Fatalf("J09 [chat] request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("J09 [chat] returned %d, want 200", resp.StatusCode)
	}
	chatResp, err := DecodeChatCompletion(resp)
	if err != nil {
		t.Fatalf("J09 [chat] decode failed: %v", err)
	}
	if len(chatResp.Choices) == 0 {
		t.Fatalf("J09 [chat] response had no choices — cascade produced no completion")
	}
	content := chatResp.Choices[0].Message.Content
	if content == "" {
		t.Fatalf("J09 [chat] empty assistant content; cascade did not reach a live provider")
	}

	// A cascade success must not have recorded a breaker failure: only
	// total cascade exhaustion (decideTerminalError wrapping
	// models.ErrLLMUnreachable) counts. Asserting CLOSED here distinguishes
	// "fell over to the secondary" from "failed everywhere".
	status := j09ReadStatus(ctx, t, client)
	if status.Breaker == nil {
		t.Fatalf("J09 [breaker] system/status carried no breaker snapshot")
	}
	if status.Breaker.State != BreakerClosed {
		t.Fatalf("J09 [breaker] state = %s after a successful cascade, want %s (failover tripped the breaker?)",
			status.Breaker.State, BreakerClosed)
	}

	t.Logf("J09-A: dead primary cascaded to a live secondary; breaker %s with %d failure(s)",
		status.Breaker.State, status.Breaker.FailureCount)
}

// TestJ09_BreakerOpens tests J09 part B: consecutive worker failures open the
// circuit breaker and produce a HUMAN handoff.
//
// This runs against the breaker profile (agentd-brk on :8770), NOT the faults
// profile. config.faults.yaml's secondary is live, so a worker call always
// cascades successfully there and the breaker can never open — see
// devenv/agentd/config.breaker.yaml, which puts two unreachable providers in
// gateway.order so the cascade is walked to exhaustion.
//
// Chat traffic is deliberately NOT used to drive this. Two reasons, both
// recorded in docs/testing/journeys.md:
//   - /v1/chat/completions answers 200 even when every provider is dead,
//     returning a "[SYSTEM] Communication with AI core timed out" message
//     (verified against :8770). So chat is not a usable failure signal.
//   - Only the queue worker records breaker failures
//     (internal/queue/worker/worker_handoffs.go's HandleGatewayError), so
//     chat traffic could not open the breaker regardless.
//
// The trip threshold is 3 consecutive failures
// (internal/queue/safety.defaultBreakerFailures), not the 5 the original
// journey spec assumed. The test asserts the observable contract — breaker
// reaches OPEN, and the failing task gains a HUMAN "Manual review required:"
// child — rather than a specific count, so it stays correct if the
// threshold is retuned.
func TestJ09_BreakerOpens(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(breakerBaseURL, "breaker")
	if err := harness.WaitForHealthy(ctx, 15*time.Second); err != nil {
		t.Fatalf("J09 [boot] breaker profile not healthy on %s: %v", breakerBaseURL, err)
	}
	client := NewAPIClient(breakerBaseURL, harness.client)

	// The breaker is process-global, not per-project: a prior run (or J07 on
	// its own profile) may have left it OPEN, which probe-gates dispatch for
	// up to safety.DefaultBreakerTimeout before any task is even attempted.
	j09ResetBreaker(ctx, t, client)

	// Three independent tasks, so the worker's failures are three separate
	// gateway round trips. max_workers defaults to NumCPU-2 (6 here), so
	// these are claimed concurrently and their failures can land close
	// together — but the breaker is mutex-guarded, so ordering is irrelevant
	// to the assertion.
	plan := DraftPlan{
		ProjectName:         UniqueProjectName("j09b"),
		Description:         "J09 breaker trip: every configured provider is unreachable",
		StartEmptyWorkspace: true,
		Tasks: []DraftTask{
			{Title: "J09 breaker task 1", Description: "Doomed: the gateway cascade will be exhausted."},
			{Title: "J09 breaker task 2", Description: "Doomed: the gateway cascade will be exhausted."},
			{Title: "J09 breaker task 3", Description: "Doomed: the gateway cascade will be exhausted."},
		},
	}
	materialized := materializePlan(ctx, t, client, "J09", plan)
	projectID := materialized.Project.ID

	j09AwaitBreakerOpen(ctx, t, client, projectID)
	humanTask := j09AwaitHandoff(ctx, t, client, projectID)
	if humanTask.Assignee != TaskAssigneeHuman {
		t.Fatalf("J09 [handoff] task %s assignee = %s, want HUMAN", humanTask.ID, humanTask.Assignee)
	}
	if want := hitlSubtaskTitleManualReview + " AI providers unavailable"; humanTask.Title != want {
		t.Fatalf("J09 [handoff] title = %q, want %q", humanTask.Title, want)
	}

	status := j09ReadStatus(ctx, t, client)
	t.Logf("J09-B: breaker %s after %d failure(s) (last error: %s); HUMAN task %s created in project %s",
		status.Breaker.State, status.Breaker.FailureCount, status.Breaker.LastError, humanTask.ID, projectID)
}

// j09AwaitBreakerOpen polls system/status until the global breaker reports
// OPEN, failing fast if it never leaves CLOSED. Polling the API rather than
// inferring from task state keeps the assertion on the thing the journey
// actually specifies (breaker OPEN), and system/status is the only surface
// that exposes it — there is no GET /api/v1/system/breaker.
func j09AwaitBreakerOpen(ctx context.Context, t *testing.T, client *APIClient, projectID string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		if status := j09ReadStatus(ctx, t, client); status.Breaker != nil {
			last = fmt.Sprintf("%s (%d failures, err=%q)", status.Breaker.State, status.Breaker.FailureCount, status.Breaker.LastError)
			if status.Breaker.State == BreakerOpen {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("J09 [breaker] breaker never opened within 60s in project %s (last state: %s) — "+
		"tasks are not reaching the gateway, or failures are not classified as breaker failures", projectID, last)
}

// j09AwaitHandoff waits for the provider-exhausted HUMAN child, releasing the
// breaker's probe gate if it is still holding this project's tasks back.
//
// The breaker is process-global, so in-flight tasks from a previous run (or
// from this run's own earlier tasks) can trip it before the tasks just
// materialized are ever dispatched. While OPEN, ProbeLimit returns 0 for
// safety.DefaultBreakerTimeout (5m), so the new tasks sit READY forever and
// no handoff is created — a flake that only shows up on repeat runs. Closing
// the breaker lets the loop dispatch them, they fail against the dead
// providers, and the breaker re-trips on their failures, which is the same
// path a first run takes.
//
// If the breaker is already CLOSED when the handoff has not appeared, the
// tasks are genuinely running rather than gated, so waiting is correct and
// the eventual timeout reports the real failure.
func j09AwaitHandoff(ctx context.Context, t *testing.T, client *APIClient, projectID string) *Task {
	t.Helper()

	poller := NewTaskPoller(client, projectID)
	deadline := time.Now().Add(60 * time.Second)
	released := false

	for time.Now().Before(deadline) {
		if task, err := poller.PollHumanHandoff(ctx); err == nil && task != nil {
			return task
		}
		if !released {
			if status := j09ReadStatus(ctx, t, client); status.Breaker != nil && status.Breaker.State == BreakerOpen {
				t.Logf("J09: breaker still OPEN with no handoff in project %s — releasing the probe gate so this project's tasks can dispatch", projectID)
				j09ResetBreaker(ctx, t, client)
				released = true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("J09 [handoff] breaker opened but no HUMAN task appeared in project %s within 60s "+
		"(released the probe gate: %v) — is healing.enabled true on this profile?", projectID, released)
	return nil
}

func j09ReadStatus(ctx context.Context, t *testing.T, client *APIClient) *SystemStatusReport {
	t.Helper()

	resp, err := client.SystemStatus(ctx)
	if err != nil {
		t.Fatalf("J09 [system/status] request failed: %v", err)
	}
	status, err := DecodeSystemStatus(resp)
	if err != nil {
		t.Fatalf("J09 [system/status] decode failed: %v", err)
	}
	return status
}

func j09ResetBreaker(ctx context.Context, t *testing.T, client *APIClient) {
	t.Helper()

	resp, err := client.ResetBreaker(ctx)
	if err != nil {
		t.Fatalf("J09 [breaker reset] request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("J09 [breaker reset] returned %d, want 200", resp.StatusCode)
	}
}
