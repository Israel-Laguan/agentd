package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"agentd/internal/models"
)

const rollingBudgetQueueMultiplier = 1.2

type ledgerEntry struct {
	at     time.Time
	tokens int
}

// RollingTokenLedger tracks cumulative token usage in a sliding time window.
type RollingTokenLedger struct {
	window  time.Duration
	limit   int
	mu      sync.Mutex
	entries []ledgerEntry
}

// NewRollingTokenLedger creates a ledger. limit 0 disables all enforcement.
func NewRollingTokenLedger(window time.Duration, limit int) *RollingTokenLedger {
	if window <= 0 {
		window = 5 * time.Hour
	}
	return &RollingTokenLedger{
		window: window,
		limit:  limit,
	}
}

// Enabled reports whether rolling budget enforcement is active.
func (l *RollingTokenLedger) Enabled() bool {
	return l != nil && l.limit > 0
}

// Limit returns the configured rolling token cap.
func (l *RollingTokenLedger) Limit() int {
	if l == nil {
		return 0
	}
	return l.limit
}

// HydrateFromEvents replaces in-memory entries from persisted per-call events.
func (l *RollingTokenLedger) HydrateFromEvents(events []models.TokenUsageEvent) {
	if l == nil || !l.Enabled() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.entries = l.entries[:0]
	for _, e := range events {
		if e.Tokens <= 0 || e.At.After(now) {
			continue
		}
		l.entries = append(l.entries, ledgerEntry{at: e.At, tokens: e.Tokens})
	}
	l.pruneLocked(now)
}

// HydrateFromStore loads token usage events within the ledger window from src.
func (l *RollingTokenLedger) HydrateFromStore(ctx context.Context, src TokenUsageEventSource) error {
	if l == nil || !l.Enabled() || src == nil {
		return nil
	}
	since := time.Now().Add(-l.window)
	events, err := src.ListTokenUsageEventsSince(ctx, since)
	if err != nil {
		return fmt.Errorf("hydrate rolling token ledger: %w", err)
	}
	l.HydrateFromEvents(events)
	return nil
}

// LogCall appends token usage and prunes entries older than the window.
func (l *RollingTokenLedger) LogCall(tokens int) {
	if l == nil || !l.Enabled() || tokens <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.pruneLocked(now)
	l.entries = append(l.entries, ledgerEntry{at: now, tokens: tokens})
}

// BudgetRemaining returns tokens still available in the window for the given limit.
func (l *RollingTokenLedger) BudgetRemaining(limit int) int {
	if l == nil || limit <= 0 {
		return limit
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(time.Now())
	used := 0
	for _, e := range l.entries {
		used += e.tokens
	}
	remaining := limit - used
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ShouldQueue returns true when remaining budget is less than 120% of projected tokens.
func (l *RollingTokenLedger) ShouldQueue(projected, limit int) bool {
	if l == nil || !l.Enabled() || projected <= 0 {
		return false
	}
	remaining := l.BudgetRemaining(limit)
	return float64(remaining) < rollingBudgetQueueMultiplier*float64(projected)
}

// EstimatedDrainWait estimates how long until enough budget frees for projected tokens.
// Task 48 will replace timer-based deferral with scheduler run_after.
func (l *RollingTokenLedger) EstimatedDrainWait(projected int) time.Duration {
	if l == nil || !l.Enabled() || projected <= 0 {
		return time.Minute
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.pruneLocked(now)
	used := 0
	for _, e := range l.entries {
		used += e.tokens
	}
	need := int(rollingBudgetQueueMultiplier*float64(projected)) - (l.limit - used)
	if need <= 0 {
		return time.Minute
	}
	if len(l.entries) == 0 {
		return l.window / 4
	}
	rate := float64(used) / l.window.Seconds()
	if rate <= 0 {
		return l.window / 4
	}
	wait := time.Duration(float64(need)/rate) * time.Second
	if wait < time.Minute {
		return time.Minute
	}
	if wait > l.window {
		return l.window
	}
	return wait
}

func (l *RollingTokenLedger) pruneLocked(now time.Time) {
	cutoff := now.Add(-l.window)
	i := 0
	for _, e := range l.entries {
		if e.at.After(cutoff) {
			l.entries[i] = e
			i++
		}
	}
	l.entries = l.entries[:i]
}
