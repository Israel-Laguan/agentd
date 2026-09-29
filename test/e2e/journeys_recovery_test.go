//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestJ07_HealingHandoff tests J07: Connector failure → HUMAN task → human
// resolution.
//
// Requires the "healing" devenv profile (agentd-healing on :8766, healing.
// enabled + outage_handoff_enabled true). devenv/agentd/config.healing.yaml
// deliberately points its only gateway provider at an address nothing
// listens on, so every queue-worker LLM call fails with ErrLLMUnreachable
// (same trick as docs/demo.md's HUMAN beat). After 3 consecutive failures
// (internal/queue/safety.defaultBreakerFailures) the circuit breaker opens
// and internal/queue/worker/worker_healing_handoff.go blocks the parent task
// and creates a "Manual review required:" HUMAN child. Materialize builds
// the DraftPlan directly (bypassing chat), since materialize never calls
// the gateway — only the worker's execution-time call ever touches the
// dead connector.
//
// The "resolution" here is POST /api/v1/tasks/{parentID}/retry, not
// POST .../human-resolution: that endpoint is reserved for manual-action
// handoffs (privileged-command approval, agentic-mode-switch, waiting-for-
// input) whose child title has the "Manual action required:" prefix — see
// internal/kanban/human_handoff.go's validateHandoffChild. A provider
// outage isn't something a human answers with text; the human fixes the
// outage out-of-band and retries the BLOCKED parent, which is exactly what
// docs/demo.md's HITL beat does (it never calls human-resolution either).
func TestJ07_HealingHandoff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	harness := NewHarness(healingBaseURL, "healing")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J07 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(healingBaseURL, harness.client)

	j07ResetBreaker(ctx, t, client)

	plan := DraftPlan{
		ProjectName:         UniqueProjectName("j07"),
		Description:         "J07 self-healing handoff smoke task",
		StartEmptyWorkspace: true,
		Tasks: []DraftTask{
			{Title: "J07 task doomed to hit the dead connector", Description: "Any LLM call this task makes will fail."},
		},
	}
	materialized := materializePlan(ctx, t, client, "J07", plan)
	projectID := materialized.Project.ID
	parentTaskID := materialized.Tasks[0].ID

	poller := NewTaskPoller(client, projectID)
	humanTask, err := poller.WaitForHumanHandoff(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("J07 [handoff] no HUMAN task appeared (breaker never opened?): %v", err)
	}
	if humanTask.State != TaskStateReady {
		t.Fatalf("J07 [handoff] HUMAN task %s state = %s, want READY (see internal/kanban/task_breakdown.go's insertReadySubtasks)", humanTask.ID, humanTask.State)
	}

	retried := j07RetryParent(ctx, t, client, parentTaskID)

	t.Logf("J07: HUMAN task %s appeared, parent %s retried back to READY", humanTask.ID, retried.ID)
}

// j07ResetBreaker resets the process-global circuit breaker before the
// journey starts. The breaker is not per-project: a prior run (or another
// journey against this same profile) may have left it OPEN, which throttles
// new task dispatch for up to breaker.handoff_after (2m default) before
// it's even attempted once (see internal/queue/loop_dispatch.go's
// dispatchAvailable). Resetting first keeps the journey fast and repeatable
// regardless of what ran before it.
func j07ResetBreaker(ctx context.Context, t *testing.T, client *APIClient) {
	t.Helper()

	resp, err := client.ResetBreaker(ctx)
	if err != nil {
		t.Fatalf("J07 [breaker reset] request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("J07 [breaker reset] returned %d, want 200", resp.StatusCode)
	}
}

func j07RetryParent(ctx context.Context, t *testing.T, client *APIClient, parentTaskID string) *Task {
	t.Helper()

	resp, err := client.RetryTask(ctx, parentTaskID)
	if err != nil {
		t.Fatalf("J07 [retry] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("J07 [retry] returned %d, want 200", resp.StatusCode)
	}
	retried, err := DecodeTaskResponse(resp)
	if err != nil {
		t.Fatalf("J07 [retry] decode failed: %v", err)
	}
	if retried.State != TaskStateReady {
		t.Fatalf("J07 [retry] parent task %s state = %s, want READY (blocked parent did not resume)", retried.ID, retried.State)
	}
	return retried
}

// TestJ08_UncleanKillRecovery tests J08: unclean kill mid-task → restart on
// the same data volume → the interrupted task is recovered.
//
// The task title carries devenv/mockllm's SLOW_TASK marker, so the worker
// runs a ~60s command and stays RUNNING long enough to SIGKILL the container
// (podman-compose's kill default). The recovered task is then legitimately
// re-dispatched, so the assertion is the RECOVERY event, not a state snapshot.
//
// KNOWN GAP (docs/testing/journeys.md J08): the intended path is
// queue.BootReconcile (internal/queue/recovery/recover.go), which resets
// RUNNING tasks whose owning PID is dead. But MarkTaskRunning stamps the
// daemon's own os.Getpid(), and in the devenv container agentd is PID 1 both
// before and after the restart, so the owner always looks alive and boot
// reconcile skips the task. It is only recovered by the stale-heartbeat sweep
// (StaleAfter, 2m default), so this test allows ~3m instead of asserting
// immediate recovery. Kept last in this file: the re-dispatched slow task
// occupies a worker for up to a minute.
func TestJ08_UncleanKillRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J08 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)

	plan := DraftPlan{
		ProjectName:         UniqueProjectName("j08"),
		Description:         "J08 unclean-kill recovery task",
		StartEmptyWorkspace: true,
		Tasks:               []DraftTask{{Title: "SLOW_TASK J08", Description: "Long-running task to interrupt."}},
	}
	materialized := materializePlan(ctx, t, client, "J08", plan)
	projectID, taskID := materialized.Project.ID, materialized.Tasks[0].ID

	poller := NewTaskPoller(client, projectID)
	if _, err := poller.WaitForTaskState(ctx, taskID, TaskStateRunning, 30*time.Second); err != nil {
		t.Fatalf("J08 [running] task never reached RUNNING: %v", err)
	}

	devenv := NewDevenvManager(composePath, "default")
	if err := devenv.KillAgentd(ctx); err != nil {
		t.Fatalf("J08 [kill] %v", err)
	}
	if err := devenv.RestartAgentd(ctx); err != nil {
		t.Fatalf("J08 [restart] %v", err)
	}
	if err := harness.WaitForHealthy(ctx, 60*time.Second); err != nil {
		t.Fatalf("J08 [restart] daemon not healthy after restart: %v", err)
	}

	restarted := time.Now()
	j08AwaitRecoveryEvent(ctx, t, client, taskID, restarted)
}

func j08AwaitRecoveryEvent(ctx context.Context, t *testing.T, client *APIClient, taskID string, restarted time.Time) {
	t.Helper()

	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		events, err := client.ListTaskEvents(ctx, taskID)
		if err == nil {
			for _, e := range events {
				if e.Type == "RECOVERY" {
					t.Logf("J08: task %s recovered %s after restart (%s)", taskID, time.Since(restarted).Round(time.Second), e.Payload)
					return
				}
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("J08 [recovery] no RECOVERY event on task %s within 180s of restart (stuck RUNNING)", taskID)
}
