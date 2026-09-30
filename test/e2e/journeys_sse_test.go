//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Internal event types for a task running to completion, mirrored from
// internal/models/enums.go. Duplicated rather than imported because the e2e
// package intentionally does not import internal/ — see test/e2e/client.go's
// chatMessage for the same reasoning.
const (
	evtTypeWarning    = "WARNING"
	evtTypeTokenUsage = "TOKEN_USAGE"
	evtTypeLogChunk   = "LOG_CHUNK"
	evtTypeResult     = "RESULT"
)

// TestJ14_SSEStreamDeliversTaskEvents tests J14: the SSE stream delivers a
// task's lifecycle events while the task runs READY → COMPLETED.
//
// The journey spec originally expected "task-started", "task-claimed" and
// "task-completed" events. Those names do not exist. The stream is a bus
// firehose (internal/api/sse/stream.go) fed by internal/bus/emitter.go, and
// the worker emits LOG_CHUNK for command output, TOKEN_USAGE for spend,
// WARNING for dispatch advisories, and RESULT on commit. Claim and start are
// not events at all: ClaimNextReadyTasks and the READY→RUNNING transition
// write no event row, so there is nothing to stream for them. This test
// asserts the events that genuinely exist, which is a stronger check than
// the spec's list: a regression in the emitter or the SSE writer fails here.
//
// Two streams are opened deliberately:
//
//   - A project-scoped stream (?project_id=…) asserts the fan-out actually
//     routes. internal/bus/emitter.go's Emit publishes each event to the
//     global topic, to "task:<id>", and — when the event carries a task ID —
//     to "project:<id>"; the handler subscribes to exactly one of those. This
//     is the path the web UI's task drawer uses.
//   - The task's own durable event log (GET /api/v1/tasks/{id}/events) is
//     then reconciled against what arrived live, so a dropped frame is
//     distinguishable from an event that was never persisted.
func TestJ14_SSEStreamDeliversTaskEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 15*time.Second); err != nil {
		t.Fatalf("J14 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)

	plan := DraftPlan{
		ProjectName:         UniqueProjectName("j14"),
		Description:         "J14 SSE lifecycle events",
		StartEmptyWorkspace: true,
		Tasks: []DraftTask{
			{Title: "J14 SSE lifecycle task", Description: "Emit log, token-usage and result signals."},
		},
	}
	materialized := materializePlan(ctx, t, client, "J14", plan)
	projectID, taskID := materialized.Project.ID, materialized.Tasks[0].ID

	// Subscribe AFTER materialize: the dispatch loop runs every 3s
	// (internal/config/cron.go's task-dispatch), so opening the stream first
	// would leave a window in which the task could complete before we are
	// listening. The durable log reconciled below is what covers that window.
	sse, err := NewSSEReader(ctx, client, projectID)
	if err != nil {
		t.Fatalf("J14 [sse] failed to open stream: %v", err)
	}
	defer func() { _ = sse.Close() }()

	poller := NewTaskPoller(client, projectID)
	if _, err := poller.WaitForAllComplete(ctx, 60*time.Second); err != nil {
		t.Fatalf("J14 [tasks] task did not reach COMPLETED: %v", err)
	}

	events := sse.DrainEvents(ctx, 2*time.Second, 10*time.Second)
	live := j14LiveTypes(events, projectID)
	durable := j14DurableTypes(ctx, t, client, taskID)

	// RESULT is the event the journey is really about: it proves the task's
	// outcome reached live subscribers rather than only the durable log. This
	// is the assertion that caught the bus gap where completions were never
	// published at all.
	if !durable[evtTypeResult] {
		t.Fatalf("J14 [events] task %s has no persisted %s event; the task lifecycle is not being recorded (durable: %v)",
			taskID, evtTypeResult, j14Keys(durable))
	}
	if !live[evtTypeResult] {
		t.Fatalf("J14 [sse] %s was persisted for task %s but never arrived on the stream (saw %v) — "+
			"live subscribers are not being told the task completed",
			evtTypeResult, taskID, j14Order(events))
	}

	// Every event the daemon actually recorded must also have streamed. This
	// is the transport assertion: a frame dropped between the emitter and the
	// SSE writer fails here even when the row is safely in the log.
	for _, typ := range []string{evtTypeLogChunk, evtTypeTokenUsage, evtTypeWarning} {
		if durable[typ] && !live[typ] {
			t.Fatalf("J14 [sse] %s was persisted for task %s but never arrived on the stream (saw %v) — "+
				"the SSE fan-out dropped a frame", typ, taskID, j14Order(events))
		}
	}

	// An event missing from BOTH is the daemon losing the write, not the
	// stream losing the frame. That used to happen under load: SQLite
	// connections had no busy_timeout (see docs/testing/journeys.md), so
	// writes failed with SQLITE_BUSY and were dropped. That is fixed, so a
	// missing row is now a regression and fails the journey.
	for _, typ := range []string{evtTypeLogChunk, evtTypeTokenUsage} {
		if !durable[typ] {
			t.Fatalf("J14 [events] task %s recorded no %s event (durable: %v) — the daemon dropped the write",
				taskID, typ, j14Keys(durable))
		}
	}
	j14AssertTokenLedger(ctx, t, client, taskID)

	j14AssertOrdering(t, events, projectID)

	t.Logf("J14: project stream delivered %d event(s) in order %v; task %s COMPLETED (durable: %v)",
		len(events), j14Order(events), taskID, j14Keys(durable))
}

// j14LiveTypes indexes the internal event types that arrived on the
// project-scoped stream. It also asserts every frame was routed to the
// project topic: the handler subscribed to "project:<id>" only, so a frame on
// any other topic means the subscription leaked the global firehose and the
// project filter is not actually filtering.
func j14LiveTypes(events []SSEEvent, projectID string) map[string]bool {
	want := "project:" + projectID
	live := map[string]bool{}
	for _, e := range events {
		sig, err := e.Signal()
		if err != nil {
			continue
		}
		if sig.Topic != want {
			continue
		}
		live[sig.Type] = true
	}
	return live
}

// j14AssertOrdering checks the lifecycle signals arrived in causal order: the
// command's output (LOG_CHUNK) before the result that reports its exit code.
// Only compares the two it can meaningfully order — TOKEN_USAGE and WARNING
// are emitted at varying points relative to each other.
func j14AssertOrdering(t *testing.T, events []SSEEvent, projectID string) {
	t.Helper()

	want := "project:" + projectID
	logAt, resultAt := -1, -1
	for i, e := range events {
		sig, err := e.Signal()
		if err != nil || sig.Topic != want {
			continue
		}
		switch sig.Type {
		case evtTypeLogChunk:
			if logAt < 0 {
				logAt = i
			}
		case evtTypeResult:
			if resultAt < 0 {
				resultAt = i
			}
		}
	}
	if logAt < 0 || resultAt < 0 {
		return // Absence is already a hard failure in the caller.
	}
	if logAt > resultAt {
		t.Fatalf("J14 [order] %s arrived at position %d, after %s at %d — output must stream before the result that reports it (order: %v)",
			evtTypeLogChunk, logAt, evtTypeResult, resultAt, j14Order(events))
	}
}

// j14DurableTypes returns the set of event types the daemon actually recorded
// for the task, which is the reference the live stream is judged against.
func j14DurableTypes(ctx context.Context, t *testing.T, client *APIClient, taskID string) map[string]bool {
	t.Helper()

	persisted, err := client.ListTaskEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("J14 [events] could not read the durable event log: %v", err)
	}
	durable := map[string]bool{}
	for _, e := range persisted {
		durable[e.Type] = true
	}
	return durable
}

// j14Order renders the internal types of a stream's frames in arrival order,
// for failure messages.
func j14Order(events []SSEEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		sig, err := e.Signal()
		if err != nil {
			out = append(out, "<undecodable>")
			continue
		}
		out = append(out, sig.Type)
	}
	return out
}

func j14Keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// mockTokensPerCall is the total_tokens the devenv mock LLM reports on every
// completion (devenv/mockllm/server.py's "usage"). The worker records one
// TOKEN_USAGE row per call, so every persisted row must carry exactly this
// figure; a different value means the ledger is mis-recording spend.
const mockTokensPerCall = 2

// j14AssertTokenLedger checks the durable TOKEN_USAGE rows for the task carry
// the token count the provider reported, not just that a row exists.
func j14AssertTokenLedger(ctx context.Context, t *testing.T, client *APIClient, taskID string) {
	t.Helper()

	persisted, err := client.ListTaskEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("J14 [ledger] could not read the durable event log: %v", err)
	}
	rows := 0
	for _, e := range persisted {
		if e.Type != evtTypeTokenUsage {
			continue
		}
		rows++
		var payload struct {
			Tokens int `json:"tokens"`
		}
		if err := json.Unmarshal([]byte(e.Payload), &payload); err != nil {
			t.Fatalf("J14 [ledger] TOKEN_USAGE %s has an unparseable payload %q: %v", e.ID, e.Payload, err)
		}
		if payload.Tokens != mockTokensPerCall {
			t.Fatalf("J14 [ledger] TOKEN_USAGE %s recorded %d tokens, want %d (the mock's reported usage)",
				e.ID, payload.Tokens, mockTokensPerCall)
		}
	}
	if rows == 0 {
		t.Fatalf("J14 [ledger] task %s has no TOKEN_USAGE rows to verify", taskID)
	}
}
