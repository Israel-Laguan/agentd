//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"strings"
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
// runs a ~30s command and stays RUNNING long enough to SIGKILL the container
// (podman-compose's kill default). The recovered task is then legitimately
// re-dispatched, so the first assertion is the RECOVERY event, not a state
// snapshot. The second is the re-run's result: the task must reach COMPLETED and
// its RESULT must carry the command's final line, which only a run that went to
// the end can produce. A RECOVERY event alone proves the reset, not that the re-run
// worked (SP-011 flagged that gap).
//
// Boot reconcile now resets the interrupted task at boot (B-008, fixed in T-030):
// MarkTaskRunning stamps the daemon's own PID, and in the devenv container agentd
// is PID 1 before and after the restart, so the liveness probe saw the owner as
// alive and skipped the task until the stale-heartbeat sweep caught it 2m later.
// BootReconcile now drops its own PID from the alive set, so the orphan is reset as
// soon as the daemon starts. The wait below is therefore a tight bound: recovery
// happens during startup, so anything near it means boot reconcile regressed to the
// stale sweep again. Kept last in this file: the re-dispatched slow task occupies a
// worker for up to a minute.
func TestJ08_UncleanKillRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// The step budgets below add up: WaitForTaskState (30s) + kill/restart +
	// WaitForHealthy (60s) + the recovery poll (30s). The deadline must cover
	// all of them, or a slow-but-successful run dies mid-poll. The re-run then
	// repeats the ~30s slow command from the start (j08RerunBudget).
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second+j08RerunBudget)
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

	// RUNNING is set before the command starts. The reset assertion below is only
	// meaningful once the first attempt has written its marker file, and its output
	// proves it has.
	j08AwaitFirstAttemptOutput(ctx, t, client, taskID)

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
	j08AwaitRerunResult(ctx, t, client, poller, taskID)
}

// j08CarriedOverMarker is printed by the mock's SLOW_TASK command when the first
// attempt's attempt.marker file is still in the workspace.
const j08CarriedOverMarker = "carried-over"

// j08AwaitFirstAttemptOutput waits until the first attempt's command has printed
// a few ticks, which happens after it created attempt.marker.
func j08AwaitFirstAttemptOutput(ctx context.Context, t *testing.T, client *APIClient, taskID string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		events, err := client.ListTaskEvents(ctx, taskID)
		if err == nil {
			for _, e := range events {
				if strings.Contains(e.Payload, "tick 2") {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("J08 [running] context expired while waiting for the first attempt's output: %v", ctx.Err())
		case <-time.After(time.Second):
		}
	}
	t.Fatalf("J08 [running] task %s never printed output within 30s, so its command did not start", taskID)
}

// j08RerunBudget covers the re-dispatched slow command: 30 one-second ticks plus
// slack for the claim tick and the result commit. It must stay well above the
// command but the command must stay under the sandbox's 60s inactivity limit (B-011),
// or the re-run is killed and never completes.
const j08RerunBudget = 60 * time.Second

// j08RerunMarker is the last line of the mock's SLOW_TASK command (devenv/mockllm
// slow_command prints "tick 0" … "tick 29"). It appears in the RESULT only when
// the command ran to the end.
const j08RerunMarker = "tick 29"

// j08AwaitRerunResult asserts the recovered task finishes: COMPLETED, with a
// RESULT event carrying the final output. The killed first attempt can never
// have produced it, so a RESULT here is the re-run's own.
func j08AwaitRerunResult(ctx context.Context, t *testing.T, client *APIClient, poller *TaskPoller, taskID string) {
	t.Helper()

	if _, err := poller.WaitForTaskState(ctx, taskID, TaskStateCompleted, j08RerunBudget); err != nil {
		t.Fatalf("J08 [rerun] recovered task %s did not reach COMPLETED within %s: %v", taskID, j08RerunBudget, err)
	}
	events, err := client.ListTaskEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("J08 [rerun] list events for task %s: %v", taskID, err)
	}
	for _, e := range events {
		if e.Type != "RESULT" {
			continue
		}
		if !strings.Contains(e.Payload, j08RerunMarker) {
			t.Fatalf("J08 [rerun] RESULT for task %s does not contain %q, so the re-run did not run to the end; payload: %q",
				taskID, j08RerunMarker, truncateForLog(e.Payload))
		}
		if strings.Contains(e.Payload, j08CarriedOverMarker) {
			t.Fatalf("J08 [reset] the re-run of task %s still saw the first attempt's attempt.marker, so recovery.clean_workspace_on_recover did not reset the workspace; payload: %q",
				taskID, truncateForLog(e.Payload))
		}
		if !j08HasEvent(events, "RECOVERY_WORKSPACE_RESET") {
			t.Fatalf("J08 [reset] task %s has no RECOVERY_WORKSPACE_RESET event (events: %d)", taskID, len(events))
		}
		t.Logf("J08: task %s re-run COMPLETED with its final output in RESULT and a reset workspace", taskID)
		return
	}
	t.Fatalf("J08 [rerun] task %s is COMPLETED but has no RESULT event (events: %d)", taskID, len(events))
}

func truncateForLog(s string) string {
	const limit = 200
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

func j08AwaitRecoveryEvent(ctx context.Context, t *testing.T, client *APIClient, taskID string, restarted time.Time) {
	t.Helper()

	// Boot reconcile runs before the daemon serves traffic, so recovery is
	// expected within seconds. 30s leaves room for a slow restart on a loaded
	// machine while still failing loudly if we are back on the 2m sweep.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("J08 [recovery] context expired (%v) while polling for a "+
				"RECOVERY event on task %s; this is a test-budget failure, not a "+
				"recovery regression", ctx.Err(), taskID)
		default:
		}
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
	t.Fatalf("J08 [recovery] no RECOVERY event on task %s within 30s of restart (boot reconcile did not recover it; a hit here means it fell back to the 2m stale-heartbeat sweep)", taskID)
}

func j08HasEvent(events []TaskEvent, eventType string) bool {
	for _, e := range events {
		if e.Type == eventType {
			return true
		}
	}
	return false
}
