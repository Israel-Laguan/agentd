package queue

import (
	"strings"
	"testing"
	"time"
)

// B-009 raised the scenario step budget from 3s to stepWaitBudget after a
// full-suite run timed out the breaker scenario. A larger budget is only safe if
// it is still bounded, so this pins that: waitFor must give up, and its error
// must say how long it waited rather than leaving the reader to guess whether a
// slow pass or a real hang produced it.

// TestWaitForGivesUpOnItsOwn guards the budget itself. It polls a condition that
// never becomes true, so it exercises the timeout path — the one no scenario
// takes today, which is exactly why it needs a test of its own. It uses a short
// explicit budget rather than stepWaitBudget, so the suite does not pay 30s to
// prove a timeout works.
func TestWaitForGivesUpOnItsOwn(t *testing.T) {
	const budget = 50 * time.Millisecond
	started := time.Now()
	err := waitForBudget(func() bool { return false }, "never happens", budget)

	if err == nil {
		t.Fatal("waitForBudget() = nil for a condition that never becomes true, want a timeout error")
	}
	if elapsed := time.Since(started); elapsed > budget+500*time.Millisecond {
		t.Fatalf("waitForBudget took %s to give up, want at most ~%s", elapsed, budget)
	}
	if want := "timed out waiting for never happens"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q is not the timeout error, want it to contain %q", err, want)
	}
	if !strings.Contains(err.Error(), budget.String()) {
		t.Fatalf("error %q does not name the budget %s, so a reader cannot tell a slow pass from a hang",
			err, budget)
	}
}

// TestStepWaitBudgetIsBounded pins the value the raised budget depends on. If
// this ever becomes unbounded the whole suite could hang, and nothing else would
// notice: every scenario that passes returns immediately and no scenario takes
// the timeout path.
func TestStepWaitBudgetIsBounded(t *testing.T) {
	if stepWaitBudget <= 0 {
		t.Fatalf("stepWaitBudget = %s, want a positive bound", stepWaitBudget)
	}
	if stepWaitBudget > time.Minute {
		t.Fatalf("stepWaitBudget = %s, want at most a minute so a hung scenario cannot stall the suite", stepWaitBudget)
	}
	// It has to be comfortably above the 3s that flaked, or the fix is cosmetic.
	if stepWaitBudget <= 3*time.Second {
		t.Fatalf("stepWaitBudget = %s, want more than the 3s that flaked in B-009", stepWaitBudget)
	}
}

// TestWaitForReturnsAsSoonAsTheConditionHolds keeps the generous budget from
// costing wall-clock time on the happy path: every scenario that passes today
// should still finish immediately.
func TestWaitForReturnsAsSoonAsTheConditionHolds(t *testing.T) {
	started := time.Now()
	if err := waitFor(func() bool { return true }, "already true"); err != nil {
		t.Fatalf("waitFor() error = %v, want nil for an already-true condition", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("waitFor took %s for an already-true condition, want an immediate return", elapsed)
	}
}

// TestWaitForNoticesALateCondition is the B-009 case directly: a condition that
// becomes true after the old 3s budget would have failed. It stands in for
// three workers taking longer than 3s to run under load.
func TestWaitForNoticesALateCondition(t *testing.T) {
	// Past the old 3s budget, so reverting to it makes this fail.
	const lateCondition = 3500 * time.Millisecond
	gate := make(chan struct{})
	go func() {
		time.Sleep(lateCondition)
		close(gate)
	}()

	if err := waitFor(func() bool {
		select {
		case <-gate:
			return true
		default:
			return false
		}
	}, "late condition"); err != nil {
		t.Fatalf("waitFor() error = %v, want nil — a condition at %s is within the budget", err, lateCondition)
	}
}
