package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestAIUsageReportSumsTheContextMixAndCountsCutLayers(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	a := aiCall(day1, "exec.turn", "m", 10)
	a.ContextMix = `{"system":{"chars":100,"messages":1},"tool_results":{"chars":40,"messages":2}}`
	a.TruncatedBy = "working_zone,agentic_head"
	b := aiCall(day1, "exec.turn", "m", 10)
	b.ContextMix = `{"system":{"chars":100,"messages":1},"memories":{"chars":5,"messages":1}}`
	old := aiCall(day1, "intake.classify_intent", "m", 10) // recorded before the mix existed
	if err := store.InsertAICalls(context.Background(), []models.AICall{a, b, old}); err != nil {
		t.Fatal(err)
	}
	rep := reportRows(t, store, models.AIUsageQuery{GroupBy: []string{models.AIDimPurpose}})
	row := rowFor(t, rep, "purpose", "exec.turn")
	for sec, want := range map[string]int{"system": 200, "tool_results": 40, "memories": 5, "history": 0} {
		if row.MixChars[sec] != want {
			t.Errorf("mix[%s] = %d, want %d", sec, row.MixChars[sec], want)
		}
	}
	if row.TruncatedCalls != 1 || row.CutBy["working_zone"] != 1 || row.CutBy["agentic_head"] != 1 || row.CutBy["strategy_budget"] != 0 {
		t.Errorf("truncated = %d, cuts = %v", row.TruncatedCalls, row.CutBy)
	}
	if got := rowFor(t, rep, "purpose", "intake.classify_intent"); got.MixChars["system"] != 0 {
		t.Errorf("a row without a mix must sum to zero, got %v", got.MixChars)
	}
}
