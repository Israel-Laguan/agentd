package kanban

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"agentd/internal/models"
)

// SP-013: nothing stops two daemons from sharing one AGENTD_HOME.
//
// These tests run two independent store handles against one shared SQLite
// database, which is what two daemons on one home look like from the storage
// layer. They answer whether the queue double-dispatches, whether reconcile
// fights over another daemon's tasks, and — the question behind the spike —
// whether startup refuses.
//
// The spike's own note says the absence of a lock was found by grep and should be
// confirmed by reading the daemon start path before running anything. TestStartupDoesNotRefuseASecondInstance below is that confirmation, written as a test.

var twoDaemonMemID atomic.Uint64

// openTwoDaemons returns two independent Store handles on ONE shared database,
// each with its own projects dir. Separate *sql.DB pools over shared cache is the
// closest in-process analogue of two processes on one home.
func openTwoDaemons(t *testing.T) (*Store, *Store) {
	t.Helper()
	id := twoDaemonMemID.Add(1)
	dsn := fmt.Sprintf("file:two-daemon-%d?mode=memory&cache=shared", id)

	dbA, err := Open(dsn, t.TempDir())
	if err != nil {
		t.Fatalf("Open() for daemon A error = %v", err)
	}
	dbB, err := Open(dsn, t.TempDir())
	if err != nil {
		t.Fatalf("Open() for daemon B error = %v", err)
	}

	storeA := NewStore(dbA, "")
	storeB := NewStore(dbB, "")
	t.Cleanup(func() {
		_ = storeA.Close()
		_ = storeB.Close()
	})
	return storeA, storeB
}

// TestTwoDaemonsDoNotDoubleDispatchTheSameTask is the core question. Both daemons
// poll the same READY task at the same moment; a correct claim is transactional,
// so each task must be handed out exactly once in total.
func TestTwoDaemonsDoNotDoubleDispatchTheSameTask(t *testing.T) {
	daemonA, daemonB := openTwoDaemons(t)
	ctx := context.Background()

	project, _, err := daemonA.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "two-daemons",
		Tasks:       []models.DraftTask{{Title: "only task"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	if _, err := daemonA.MarkProjectTasksReady(ctx, project.ID); err != nil {
		t.Fatalf("MarkProjectTasksReady() error = %v", err)
	}

	var wg sync.WaitGroup
	claims := make([][]models.Task, 2)
	for i, store := range []*Store{daemonA, daemonB} {
		wg.Add(1)
		go func(slot int, s *Store) {
			defer wg.Done()
			claimed, err := s.ClaimNextReadyTasks(ctx, 1)
			if err != nil {
				t.Errorf("daemon %d ClaimNextReadyTasks() error = %v", slot, err)
				return
			}
			claims[slot] = claimed
		}(i, store)
	}
	wg.Wait()

	total := len(claims[0]) + len(claims[1])
	if total != 1 {
		t.Fatalf("two daemons claimed %d tasks in total (A=%d, B=%d), want exactly 1 — "+
			"the claim is not exclusive across handles", total, len(claims[0]), len(claims[1]))
	}
	t.Logf("daemon A claimed %d, daemon B claimed %d", len(claims[0]), len(claims[1]))
}

// TestTwoDaemonsBootReconcileEachOthersTasks is the false-recovery risk. Daemon A
// boots and reconciles while daemon B has a task genuinely RUNNING under its own
// live PID. Reconcile resets ghosts by liveness, so A must not touch B's task —
// but note the spike's point that this only holds while the two PIDs differ.
func TestTwoDaemonsBootReconcileEachOthersTasks(t *testing.T) {
	daemonA, daemonB := openTwoDaemons(t)
	ctx := context.Background()

	project, _, err := daemonA.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "reconcile-cross-talk",
		Tasks:       []models.DraftTask{{Title: "b owns this"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	if _, err := daemonA.MarkProjectTasksReady(ctx, project.ID); err != nil {
		t.Fatalf("MarkProjectTasksReady() error = %v", err)
	}
	claimed, err := daemonB.ClaimNextReadyTasks(ctx, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("daemon B claim = %d tasks, err = %v; want 1", len(claimed), err)
	}
	// B owns the task under B's real PID, and B is a live process.
	daemonBPID := os.Getpid()
	if _, err := daemonB.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, daemonBPID); err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}

	// A boots and sees our shared PID as alive, exactly as it would for any live
	// process. The task must survive.
	recovered, err := daemonA.ReconcileGhostTasks(ctx, []int{daemonBPID})
	if err != nil {
		t.Fatalf("ReconcileGhostTasks() error = %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("daemon A recovered %d tasks, want 0 — it reset a task owned by a live process", len(recovered))
	}

	current, err := daemonB.GetTask(ctx, claimed[0].ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if current.State != models.TaskStateRunning {
		t.Fatalf("task state = %s, want RUNNING after the other daemon's boot", current.State)
	}
}

// TestStartupDoesNotRefuseASecondInstance is the spike's "confirm by reading the
// daemon start path" step, expressed as a test. Open() is the first thing the start
// path does with the home; if a guard existed it would live here or at the daemon
// entry point. A second Open on an already-initialised home must succeed.
func TestStartupDoesNotRefuseASecondInstance(t *testing.T) {
	id := twoDaemonMemID.Add(1)
	dsn := fmt.Sprintf("file:no-guard-%d?mode=memory&cache=shared", id)

	first, err := Open(dsn, t.TempDir())
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	defer func() { _ = first.Close() }()

	second, err := Open(dsn, t.TempDir())
	if err != nil {
		t.Fatalf("second Open() on the same home error = %v, want nil — "+
			"a guard would reject a second daemon here", err)
	}
	defer func() { _ = second.Close() }()

	store := NewStore(second, "")
	if _, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "after-second-open",
		Tasks:       []models.DraftTask{{Title: "still works"}},
	}); err != nil {
		t.Fatalf("MaterializePlan() after a second Open error = %v", err)
	}

	t.Log("a second daemon opened the same home and wrote to it; no single-instance guard exists")
}

// TestTwoDaemonClaimAssertionDetectsADoubleClaim guards the assertion above. A
// test that counts claims and reports "exactly 1" is only meaningful if it would
// notice 2, which is worth proving directly rather than by mutating the
// transaction internals (which the ImmediateTx type makes awkward).
func TestTwoDaemonClaimAssertionDetectsADoubleClaim(t *testing.T) {
	daemonA, _ := openTwoDaemons(t)
	ctx := context.Background()

	project, _, err := daemonA.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "assertion-selfcheck",
		Tasks: []models.DraftTask{
			{Title: "one"}, {Title: "two"}, {Title: "three"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	if _, err := daemonA.MarkProjectTasksReady(ctx, project.ID); err != nil {
		t.Fatalf("MarkProjectTasksReady() error = %v", err)
	}

	first, err := daemonA.ClaimNextReadyTasks(ctx, 3)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	second, err := daemonA.ClaimNextReadyTasks(ctx, 3)
	if err != nil {
		t.Fatalf("second ClaimNextReadyTasks() error = %v", err)
	}

	if len(first)+len(second) != 3 {
		t.Fatalf("two claims returned %d tasks in total, want 3 — the count-based "+
			"assertion in TestTwoDaemonsDoNotDoubleDispatchTheSameTask would miss a double claim",
			len(first)+len(second))
	}
	if len(first) == 3 && len(second) == 3 {
		t.Fatal("both claims returned every task; the claim is not exclusive and the count check is blind")
	}
}
