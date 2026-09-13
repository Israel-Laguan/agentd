package main

import (
	"context"
	"log/slog"

	"agentd/internal/models"
	"agentd/internal/queue"
)

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
