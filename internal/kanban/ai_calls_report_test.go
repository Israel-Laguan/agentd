package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

var day1 = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
var day2 = time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)

func seedReport(t *testing.T, store *Store) {
	t.Helper()
	repair := aiCall(day1, "exec.turn", "m1", 200)
	repair.Attempt, repair.TaskID, repair.CorrelationID, repair.LatencyMS = "json_repair", "t1", "req-1", 300
	cascade := aiCall(day1, "exec.turn", "m2", 100)
	cascade.Cascade, cascade.Outcome, cascade.TaskID, cascade.CorrelationID = true, "failed", "t1", "req-1"
	first := aiCall(day1, "exec.turn", "m1", 50)
	first.TaskID, first.CorrelationID = "t1", "req-1"
	other := aiCall(day2, "intake.classify_intent", "m1", 10)
	other.CorrelationID, other.LatencyMS = "req-2", 40
	if err := store.InsertAICalls(context.Background(), []models.AICall{repair, cascade, first, other}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func reportRows(t *testing.T, store *Store, q models.AIUsageQuery) models.AIUsageReport {
	t.Helper()
	rep, err := store.AIUsageReport(context.Background(), q)
	if err != nil {
		t.Fatalf("AIUsageReport: %v", err)
	}
	return rep
}

func rowFor(t *testing.T, rep models.AIUsageReport, dim, value string) models.AIUsageRow {
	t.Helper()
	for _, r := range rep.Rows {
		if r.Group[dim] == value {
			return r
		}
	}
	t.Fatalf("no row with %s=%q in %+v", dim, value, rep.Rows)
	return models.AIUsageRow{}
}

func TestAIUsageReportGroupsByPurposeWithRates(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	seedReport(t, store)
	rep := reportRows(t, store, models.AIUsageQuery{GroupBy: []string{models.AIDimPurpose}})
	if len(rep.Rows) != 2 {
		t.Fatalf("rows = %+v, want 2 purposes", rep.Rows)
	}
	exec := rowFor(t, rep, "purpose", "exec.turn")
	if exec.Calls != 3 || exec.Tokens != 350 || exec.Failed != 1 || exec.MaxLatencyMS != 300 {
		t.Fatalf("exec row = %+v", exec)
	}
	if exec.FirstAttempts != 2 || exec.RepairAttempts != 1 || exec.RepairRate != 0.5 {
		t.Fatalf("repair = %d/%d rate %v, want 1/2 rate 0.5", exec.RepairAttempts, exec.FirstAttempts, exec.RepairRate)
	}
	if exec.CascadeCalls != 1 || exec.CascadeRate < 0.33 || exec.CascadeRate > 0.34 {
		t.Fatalf("cascade = %d rate %v, want 1 rate 1/3", exec.CascadeCalls, exec.CascadeRate)
	}
	if rep.Totals.Calls != 4 || rep.Totals.Tokens != 360 {
		t.Fatalf("totals = %+v", rep.Totals)
	}
}

func TestAIUsageReportGroupsByStageModelDayTaskAndRequest(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	seedReport(t, store)
	cases := []struct {
		dim, value string
		tokens     int
	}{
		{models.AIDimStage, "exec", 350},
		{models.AIDimModel, "m2", 100},
		{models.AIDimDay, "2026-10-18", 350},
		{models.AIDimTask, "t1", 350},
		{models.AIDimRequest, "req-2", 10},
	}
	for _, c := range cases {
		rep := reportRows(t, store, models.AIUsageQuery{GroupBy: []string{c.dim}})
		key := c.dim
		if got := rowFor(t, rep, key, c.value).Tokens; got != c.tokens {
			t.Errorf("%s=%s tokens = %d, want %d", c.dim, c.value, got, c.tokens)
		}
	}
}

func TestAIUsageReportFiltersAndCombinedGrouping(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	seedReport(t, store)
	rep := reportRows(t, store, models.AIUsageQuery{
		From: day1, To: day2, GroupBy: []string{models.AIDimDay, models.AIDimPurpose}, CorrelationID: "req-1",
	})
	if len(rep.Rows) != 1 || rep.Rows[0].Group["day"] != "2026-10-18" || rep.Rows[0].Group["purpose"] != "exec.turn" {
		t.Fatalf("rows = %+v, want one day+purpose row", rep.Rows)
	}
	if got := reportRows(t, store, models.AIUsageQuery{TaskID: "t1"}).Totals.Calls; got != 3 {
		t.Fatalf("task filter calls = %d, want 3", got)
	}
	if got := reportRows(t, store, models.AIUsageQuery{From: day2}).Totals.Calls; got != 1 {
		t.Fatalf("from filter calls = %d, want 1", got)
	}
}

func TestAIUsageReportEmptyTableAndBadQuery(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	rep := reportRows(t, store, models.AIUsageQuery{GroupBy: []string{models.AIDimPurpose}})
	if len(rep.Rows) != 0 || rep.Totals.Calls != 0 || rep.Rows == nil {
		t.Fatalf("empty report = %+v, want no rows (non-nil) and zero totals", rep)
	}
	if _, err := store.AIUsageReport(context.Background(), models.AIUsageQuery{GroupBy: []string{"nope"}}); err == nil {
		t.Fatal("unknown group_by accepted")
	}
}

func TestAIUsageReportLabelsTheRemovedLegacyPurpose(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	old := aiCall(day1, "exec.legacy_command", "m1", 30)
	if err := store.InsertAICalls(context.Background(), []models.AICall{old, aiCall(day1, "exec.turn", "m1", 5)}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rep := reportRows(t, store, models.AIUsageQuery{GroupBy: []string{models.AIDimPurpose}})
	if row := rowFor(t, rep, "purpose", "legacy (removed)"); row.Calls != 1 || row.Tokens != 30 {
		t.Fatalf("legacy row = %+v", row)
	}
	rowFor(t, rep, "purpose", "exec.turn")
}
