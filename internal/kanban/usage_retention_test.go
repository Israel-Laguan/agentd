package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

// T-056: ai_calls and tool_calls grow without bound. Pruning removes rows older
// than the cut and keeps newer rows, so the report totals for recent days stay.
func TestPruneUsageBeforeKeepsNewerRows(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	cut := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	old := cut.AddDate(0, 0, -30)
	recent := cut.AddDate(0, 0, 5)
	for _, at := range []time.Time{old, recent} {
		if err := store.InsertAICalls(ctx, []models.AICall{aiCall(at, "exec.turn", "m", 100)}); err != nil {
			t.Fatalf("InsertAICalls: %v", err)
		}
		if err := store.InsertToolCall(ctx, models.ToolCall{At: at, TaskID: "t1", CallID: "c", Tool: "read", Status: "ok", ResultBytes: 10}); err != nil {
			t.Fatalf("InsertToolCall: %v", err)
		}
	}

	removed, err := store.PruneUsageBefore(ctx, cut)
	if err != nil {
		t.Fatalf("PruneUsageBefore: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2 (one ai_call and one tool_call)", removed)
	}
	calls, err := store.AIUsageReport(ctx, models.AIUsageQuery{})
	if err != nil {
		t.Fatalf("AICallsReport: %v", err)
	}
	if len(calls.Rows) != 1 || calls.Totals.Calls != 1 {
		t.Fatalf("report after prune = %+v, want only the recent call", calls.Totals)
	}
}
