package kanban

import (
	"context"
	"fmt"
	"testing"

	"agentd/internal/models"
)

// Gap A (B-013): the requeue a probe-in-flight sibling performs must be lossless
// against the real store.
//
// While a provider breaker's probe is in flight, gateProvider calls
// w.requeue(ctx, task, "") for every sibling (internal/queue/worker), which is
// exactly one store write — UpdateTaskState(RUNNING -> READY) with no retry
// bookkeeping — and nothing else. The rest of the contract is covered where its
// own layer lives: the AdmissionProbeInFlight decision by the breaker's own
// tests, and the gate's choice to requeue rather than hand off by the worker
// tests. Neither can see this half, and neither can the fake stores: they accept
// any write and never enforce the optimistic-lock version. The depguard rules
// queue-test-isolation and kanban-test-isolation keep the real board and the
// breaker package apart, so this is where the store half belongs.
//
// What matters about that write:
//   - the task lands in READY so the next claim tick sees it,
//   - RetryCount stays 0, since an empty requeue payload emits no RETRY event and
//     a bumped count would slowly evict healthy siblings,
//   - started_at and last_heartbeat survive, because a sibling that waited behind
//     a slow probe must not look like a dead run to the stale-heartbeat sweep,
//   - and it repeats cleanly, since the dispatch loop does it once per tick for
//     as long as the probe runs.
func TestSiblingsRequeueLosslesslyWhileProbeInFlight(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const siblings = 5
	if _, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "probe-siblings",
		Tasks:       siblingDrafts(siblings),
	}); err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	// Three cycles, standing in for three dispatch ticks that each found the
	// provider's probe still in flight. Repeating is the point: the transition
	// runs once per tick for as long as the probe lasts.
	for cycle := range 3 {
		running := claimAndStartSiblings(t, store, ctx, cycle, siblings)
		for i := range running {
			// Exactly what worker.requeue(ctx, task, "") does.
			requeued, err := store.UpdateTaskState(ctx, running[i].ID, running[i].UpdatedAt, models.TaskStateReady)
			if err != nil {
				t.Fatalf("cycle %d: requeue of %s error = %v", cycle, running[i].ID, err)
			}
			assertRequeuedSibling(t, cycle, &running[i], requeued)
		}
	}

	// And the siblings must be claimable and startable again afterwards.
	claimed, err := store.ClaimNextReadyTasks(ctx, siblings)
	if err != nil || len(claimed) != siblings {
		t.Fatalf("final ClaimNextReadyTasks() = (%d tasks, %v), want (%d, nil)", len(claimed), err, siblings)
	}
	for _, task := range claimed {
		if _, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 4243); err != nil {
			t.Fatalf("MarkTaskRunning(%s) after the requeue cycles error = %v — the requeued row is not "+
				"startable", task.ID, err)
		}
	}
}

// claimAndStartSiblings claims every sibling and starts it, which is what the
// dispatch loop does before the gate refuses it: each row must come back as
// QUEUED from the claim, proving the previous cycle's requeue really did return
// it to READY where the claim query can see it.
func claimAndStartSiblings(t *testing.T, store *Store, ctx context.Context, cycle, want int) []models.Task {
	t.Helper()
	claimed, err := store.ClaimNextReadyTasks(ctx, want)
	if err != nil {
		t.Fatalf("cycle %d: ClaimNextReadyTasks() error = %v", cycle, err)
	}
	if len(claimed) != want {
		t.Fatalf("cycle %d: claimed %d siblings, want %d — the last requeue did not return every sibling to READY",
			cycle, len(claimed), want)
	}
	running := make([]models.Task, 0, want)
	for _, task := range claimed {
		row, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 4242)
		if err != nil {
			t.Fatalf("cycle %d: MarkTaskRunning(%s) error = %v", cycle, task.ID, err)
		}
		running = append(running, *row)
	}
	return running
}

func siblingDrafts(n int) []models.DraftTask {
	drafts := make([]models.DraftTask, n)
	for i := range drafts {
		drafts[i] = models.DraftTask{TempID: fmt.Sprintf("s%d", i), Title: fmt.Sprintf("sibling %d", i)}
	}
	return drafts
}

// assertRequeuedSibling pins what RUNNING -> READY leaves on the row.
//
// db.UpdateTaskStateInTx stamps started_at only on entry to RUNNING and clears
// os_process_id/last_heartbeat only when the next state is BLOCKED, so a requeue
// must preserve both. Were that to change, a sibling that waited behind a slow
// probe would carry no heartbeat and the stale sweep would recover it as a dead
// run — a failure the fake-store tests cannot observe at all.
func assertRequeuedSibling(t *testing.T, cycle int, running, requeued *models.Task) {
	t.Helper()
	where := fmt.Sprintf("cycle %d (%s)", cycle, running.ID)

	if requeued.State != models.TaskStateReady {
		t.Fatalf("%s: state = %s, want READY", where, requeued.State)
	}
	if requeued.RetryCount != 0 {
		t.Fatalf("%s: retry_count = %d, want 0 — waiting on a probe is not a retry", where, requeued.RetryCount)
	}
	if running.StartedAt == nil || running.LastHeartbeat == nil {
		t.Fatalf("%s: was RUNNING without started_at/last_heartbeat", where)
	}
	if requeued.StartedAt == nil || !requeued.StartedAt.Equal(*running.StartedAt) {
		t.Fatalf("%s: started_at = %v, want the RUNNING value %v", where, requeued.StartedAt, running.StartedAt)
	}
	if requeued.LastHeartbeat == nil || !requeued.LastHeartbeat.Equal(*running.LastHeartbeat) {
		t.Fatalf("%s: last_heartbeat = %v, want the RUNNING value %v — clearing it would make the "+
			"stale-heartbeat sweep treat a waiting sibling as a dead run",
			where, requeued.LastHeartbeat, running.LastHeartbeat)
	}
	if requeued.CompletedAt != nil {
		t.Fatalf("%s: completed_at = %v, want nil", where, requeued.CompletedAt)
	}
	if !requeued.UpdatedAt.After(running.UpdatedAt) {
		t.Fatalf("%s: updated_at = %v did not advance past %v, so the optimistic-lock version never moved",
			where, requeued.UpdatedAt, running.UpdatedAt)
	}
}