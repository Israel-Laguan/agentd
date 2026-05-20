package queue

import (
	"testing"
	"time"
)

func TestRollingTokenLedger_PruneAndRemaining(t *testing.T) {
	t.Parallel()
	l := NewRollingTokenLedger(time.Hour, 1000)
	l.LogCall(400)
	l.LogCall(300)
	if rem := l.BudgetRemaining(1000); rem != 300 {
		t.Fatalf("BudgetRemaining = %d, want 300", rem)
	}
}

func TestRollingTokenLedger_ShouldQueue(t *testing.T) {
	t.Parallel()
	l := NewRollingTokenLedger(time.Hour, 1000)
	l.LogCall(900)
	if !l.ShouldQueue(100, 1000) {
		t.Fatal("expected ShouldQueue when remaining < 120% projected")
	}
	l.LogCall(-800) // not possible via LogCall; reset
	l = NewRollingTokenLedger(time.Hour, 1000)
	l.LogCall(100)
	if l.ShouldQueue(100, 1000) {
		t.Fatal("expected ShouldQueue false with ample budget")
	}
}

func TestRollingTokenLedger_Disabled(t *testing.T) {
	t.Parallel()
	l := NewRollingTokenLedger(time.Hour, 0)
	if l.Enabled() || l.ShouldQueue(100, 0) {
		t.Fatal("limit 0 should disable ledger")
	}
}

func TestRollingTokenLedger_EstimatedDrainWait(t *testing.T) {
	t.Parallel()
	l := NewRollingTokenLedger(time.Hour, 1000)
	l.LogCall(950)
	wait := l.EstimatedDrainWait(100)
	if wait < time.Minute {
		t.Fatalf("EstimatedDrainWait = %v, want at least 1m", wait)
	}
}
