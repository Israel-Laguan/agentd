package main

import (
	"context"
	"log/slog"

	"agentd/internal/models"
	"agentd/internal/queue"
)

// tokenUsageStore extracts the queue.TokenUsageStore narrow interface from the
// KanbanStore. Returns nil if the concrete store does not implement AddTokenUsage
// (e.g. test doubles), in which case per-call token persistence is a no-op.
func tokenUsageStore(store models.KanbanStore) queue.TokenUsageStore {
	ts, _ := store.(queue.TokenUsageStore)
	return ts
}

func hydrateRollingLedger(ctx context.Context, store models.KanbanStore, ledger *queue.RollingTokenLedger) {
	if ledger == nil || !ledger.Enabled() {
		return
	}
	src, ok := store.(queue.TokenUsageEventSource)
	if !ok {
		return
	}
	if err := ledger.HydrateFromStore(ctx, src); err != nil {
		slog.Warn("rolling token ledger hydrate failed", "err", err)
	}
}
