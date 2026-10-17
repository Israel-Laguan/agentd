package db

import (
	"testing"
	"time"
)

// TestBusyRetryBudgetPinsLedgerLockCeiling pins the longest SQLite write lock the
// token ledger survives: maxBusyRetries attempts, each with a fresh busy_timeout
// window (see TestRetryOnBusyToleratesLockHeldLongerThanOneTimeout), so about 30s.
//
// Accepted limit (S08): a lock held longer than this still drops a TOKEN_USAGE
// row. The follow-up, deterministic ledger event IDs, is due only if a row is
// seen dropped this way or SQLite's BUSY handling changes; the tagged spike
// TestSpikeRetryOnBusyCeiling (internal/kanban, -tags=busywritespike) measures
// the drop. busy_timeout itself is pinned by TestOpenAppliesBusyTimeoutToEveryConnection.
func TestBusyRetryBudgetPinsLedgerLockCeiling(t *testing.T) {
	const busyTimeout = 5 * time.Second
	const want = 30 * time.Second
	if got := time.Duration(maxBusyRetries) * busyTimeout; got != want {
		t.Fatalf("ledger lock ceiling = %v (maxBusyRetries=%d x busy_timeout %v), want %v; "+
			"changing it moves the accepted TOKEN_USAGE drop limit: re-run TestSpikeRetryOnBusyCeiling "+
			"and update this test and its comment", got, maxBusyRetries, busyTimeout, want)
	}
}
