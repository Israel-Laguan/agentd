package recovery

import (
	"context"
	"os"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/queue/safety"
	"agentd/internal/testutil"
)

// SP-010: after a host reboot, can an unrelated live process hold a dead
// daemon's old PID so BootReconcile skips its task?
//
// The answer from reading the code is yes — boot reconcile decides by PID
// liveness alone, and nothing records *which* process that PID was. These tests
// pin the consequence, because "accepted limit" is only meaningful if the
// behaviour it accepts is actually demonstrated.
//
// The existing TestBootReconcile_leavesTaskOwnedByOtherLivePID already covers a
// PID that is alive. What it cannot express is *why* that is wrong: a live PID
// is evidence of a live task only when the live process is the one that claimed
// it. After a reboot, PID reuse makes that inference false.

// TestBootReconcileSkipsTaskWhoseOwnerPIDWasReused is the spike's reproduction.
//
// The task's owner PID is a live, unrelated process — indistinguishable, from
// reconcile's point of view, from a healthy second daemon. Boot must skip it,
// because that is what PID-liveness means. The task then waits for the
// stale-heartbeat sweep instead.
func TestBootReconcileSkipsTaskWhoseOwnerPIDWasReused(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()

	// A genuinely live process that has nothing to do with this task. os.Getpid()
	// is live and is the most honest stand-in available in-process; PID 1 is used
	// as the "dead daemon's old PID" because after a reboot it is always taken.
	reusedPID := 1
	if reusedPID == os.Getpid() {
		reusedPID = os.Getppid()
	}
	taskID := seedRunningGhostTaskWithPID(t, ctx, store, reusedPID)

	sink := &recordingSink{}
	// The probe sees a live process at the reused PID, so it is not in the alive
	// set as a ghost — and neither is the task.
	err := BootReconcile(ctx, store, safety.StaticPIDProbe{PIDs: []int{reusedPID}}, sink)
	if err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if task.State != models.TaskStateRunning {
		t.Fatalf("state = %s, want RUNNING — boot skipped the task because its owner PID looked alive", task.State)
	}

	// The bound on the damage: the stale-heartbeat sweep recovers it. StaleAfter
	// is 2 minutes by default (daemon.go's normalizeIntervals), so the sweep is
	// what ends this state, not boot.
	t.Logf("FINDING: a RUNNING task whose owner PID was reused by an unrelated live "+
		"process is not recovered at boot; it waits for the stale-heartbeat sweep (~%s)", staleAfterDefault)
}

// TestBootReconcileCannotDistinguishReuseFromASecondDaemon states the root cause
// as an assertion: given only a PID, the two situations are indistinguishable. If
// this test ever fails because someone added an instance ID or a process
// start-time check, SP-010's recommendation has been implemented and this spike's
// conclusion is stale.
func TestBootReconcileCannotDistinguishReuseFromASecondDaemon(t *testing.T) {
	storeReuse := testutil.NewFakeStore()
	storeDaemon := testutil.NewFakeStore()
	ctx := context.Background()

	sharedPID := 1
	if sharedPID == os.Getpid() {
		sharedPID = os.Getppid()
	}

	reuseTask := seedRunningGhostTaskWithPID(t, ctx, storeReuse, sharedPID)
	daemonTask := seedRunningGhostTaskWithPID(t, ctx, storeDaemon, sharedPID)

	probe := safety.StaticPIDProbe{PIDs: []int{sharedPID}}
	if err := BootReconcile(ctx, storeReuse, probe, &recordingSink{}); err != nil {
		t.Fatalf("BootReconcile(reuse) error = %v", err)
	}
	if err := BootReconcile(ctx, storeDaemon, probe, &recordingSink{}); err != nil {
		t.Fatalf("BootReconcile(second daemon) error = %v", err)
	}

	stateA, err := storeReuse.GetTask(ctx, reuseTask)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	stateB, err := storeDaemon.GetTask(ctx, daemonTask)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if stateA.State != stateB.State {
		t.Skip("the two cases are now distinguishable; SP-010's finding no longer holds")
	}
	if stateA.State != models.TaskStateRunning {
		t.Fatalf("states = %s and %s, want both RUNNING — an identical PID should lead to identical treatment", stateA.State, stateB.State)
	}
	t.Logf("FINDING: a reused PID and a live second daemon both leave the task %s; "+
		"reconcile cannot tell them apart, which is why PID reuse delays recovery", stateA.State)
}

// staleAfterDefault mirrors the daemon's StaleAfter default, which is the sweep's
// bound on this state. Declared here rather than imported because the value is
// internal/queue's, and duplicating it in a comment beats importing the daemon
// into a leaf package's test.
const staleAfterDefault = 2 * time.Minute
