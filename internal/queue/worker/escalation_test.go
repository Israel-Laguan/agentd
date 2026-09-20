package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestClassifyVerifyOutcome_Pass(t *testing.T) {
	result := VerifyResult{
		Results: []CheckResult{
			{Check: "go test ./...", Outcome: "pass", Detail: ""},
		},
		Overall: "pass",
	}
	outcome := ClassifyVerifyOutcome(result)
	if outcome != models.VerifyOutcomePass {
		t.Errorf("expected pass, got %v", outcome)
	}
}

func TestClassifyVerifyOutcome_Fail(t *testing.T) {
	result := VerifyResult{
		Results: []CheckResult{
			{Check: "go test ./...", Outcome: "fail", Detail: "test failed"},
		},
		Overall: "fail",
	}
	outcome := ClassifyVerifyOutcome(result)
	if outcome != models.VerifyOutcomeFail {
		t.Errorf("expected fail, got %v", outcome)
	}
}

func TestClassifyVerifyOutcome_Flake(t *testing.T) {
	result := VerifyResult{
		Results: []CheckResult{
			{Check: "go test ./...", Outcome: "flake", Detail: "intermittent"},
			{Check: "go vet ./...", Outcome: "pass", Detail: ""},
		},
		Overall: "fail",
	}
	outcome := ClassifyVerifyOutcome(result)
	if outcome != models.VerifyOutcomeFlake {
		t.Errorf("expected flake, got %v", outcome)
	}
}

func TestClassifyVerifyOutcome_Conflict(t *testing.T) {
	result := VerifyResult{
		Results: []CheckResult{
			{Check: "merge conflicts", Outcome: "conflict", Detail: "merge conflict detected"},
		},
		Overall: "fail",
	}
	outcome := ClassifyVerifyOutcome(result)
	if outcome != models.VerifyOutcomeConflict {
		t.Errorf("expected conflict, got %v", outcome)
	}
}

// newTieredPipelineFixture materializes an origin task and marks it RUNNING,
// the state a real tiered origin sits in for the whole pipeline's duration.
func newTieredPipelineFixture(t *testing.T, store *testutil.FakeKanbanStore) models.Task {
	t.Helper()
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "escalation-fixture",
		Tasks:       []models.DraftTask{{Title: "origin", Description: "complex task"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	origin, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark origin running: %v", err)
	}
	return *origin
}

func TestHandleVerifyOutcome_Pass(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	w := &Worker{store: store, sink: &mockEventSink{}}

	origin := newTieredPipelineFixture(t, store)
	verifyTask := models.Task{BaseEntity: models.BaseEntity{ID: "verify-1"}, Title: "verify step"}

	if err := w.handleVerifyOutcome(ctx, verifyTask, models.VerifyOutcomePass, VerifyResult{Overall: "pass"}, origin); err != nil {
		t.Fatalf("handleVerifyOutcome() error = %v", err)
	}

	got, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateCompleted {
		t.Fatalf("origin state = %s, want COMPLETED", got.State)
	}
}

func TestVerifyResultOutcome_Valid(t *testing.T) {
	tests := []struct {
		outcome models.VerifyResultOutcome
		want    bool
	}{
		{models.VerifyOutcomePass, true},
		{models.VerifyOutcomeFail, true},
		{models.VerifyOutcomeFlake, true},
		{models.VerifyOutcomeConflict, true},
		{models.VerifyResultOutcome("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.outcome), func(t *testing.T) {
			if got := tt.outcome.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- getMetadata/setMetadata round trip ---------------------------------

func TestMetadata_RoundTripsThroughTaskLogs(t *testing.T) {
	task := models.Task{}

	if got := getMetadataInt(task, "mid_fix_passes", 0); got != 0 {
		t.Fatalf("getMetadataInt() on empty task.Logs = %d, want default 0", got)
	}

	setMetadataInt(&task, "mid_fix_passes", 1)
	if task.Logs == "" {
		t.Fatal("setMetadataInt did not write task.Logs")
	}
	if got := getMetadataInt(task, "mid_fix_passes", 0); got != 1 {
		t.Fatalf("getMetadataInt() after first set = %d, want 1", got)
	}

	// Increment again on the same task — the whole point of the "persists
	// and increments across calls" requirement.
	setMetadataInt(&task, "mid_fix_passes", 2)
	if got := getMetadataInt(task, "mid_fix_passes", 0); got != 2 {
		t.Fatalf("getMetadataInt() after second set = %d, want 2", got)
	}

	// A second, independent key must not clobber the first.
	setMetadataInt(&task, "escalate_count", 1)
	if got := getMetadataInt(task, "mid_fix_passes", 0); got != 2 {
		t.Fatalf("mid_fix_passes clobbered by setting escalate_count: got %d, want 2", got)
	}
	if got := getMetadataInt(task, "escalate_count", 0); got != 1 {
		t.Fatalf("getMetadataInt(escalate_count) = %d, want 1", got)
	}
}

func TestGetMetadata_MissingKeyReturnsDefault(t *testing.T) {
	task := models.Task{}
	setMetadataInt(&task, "mid_fix_passes", 1)
	if got := getMetadata(task, "nonexistent", "fallback"); got != "fallback" {
		t.Fatalf("getMetadata() for missing key = %q, want %q", got, "fallback")
	}
}

// --- mid-fix / escalate caps, enforced against the durable DAG ----------

func countSpawnedByTitlePrefix(t *testing.T, store *testutil.FakeKanbanStore, originID, prefix string) int {
	t.Helper()
	children, err := store.ListChildTasksByRelation(context.Background(), originID, models.TaskRelationSpawnedBy)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation: %v", err)
	}
	count := 0
	for _, c := range children {
		if strings.HasPrefix(c.Title, prefix) {
			count++
		}
	}
	return count
}

func countSpawnedByAgentID(t *testing.T, store *testutil.FakeKanbanStore, originID, agentID string) int {
	t.Helper()
	children, err := store.ListChildTasksByRelation(context.Background(), originID, models.TaskRelationSpawnedBy)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation: %v", err)
	}
	count := 0
	for _, c := range children {
		if c.AgentID == agentID {
			count++
		}
	}
	return count
}

func TestScheduleMidFix_EscalatesOnceCapReached(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	w := &Worker{
		store: store,
		sink:  &mockEventSink{},
		tieredCfg: config.TieredConfig{
			Escalation: config.TieredEscalationConfig{MaxMidFix: 2, MaxEscalate: 1},
		},
	}
	origin := newTieredPipelineFixture(t, store)
	verifyTask := models.Task{BaseEntity: models.BaseEntity{ID: "verify-1"}, Title: "verify: origin"}

	// Attempts 1 and 2 stay under the cap: each spawns a mid-fix redo.
	for i := 1; i <= 2; i++ {
		if err := w.scheduleMidFix(ctx, verifyTask, origin, VerifyResult{}); err != nil {
			t.Fatalf("scheduleMidFix attempt %d: %v", i, err)
		}
	}
	if got := countSpawnedByTitlePrefix(t, store, origin.ID, midFixTitlePrefix); got != 2 {
		t.Fatalf("mid-fix children after 2 attempts = %d, want 2", got)
	}
	if got := countSpawnedByAgentID(t, store, origin.ID, escalateAgentID); got != 0 {
		t.Fatalf("escalate children after 2 mid-fix attempts = %d, want 0", got)
	}

	// The 3rd attempt must route to escalation, not a 3rd mid-fix.
	if err := w.scheduleMidFix(ctx, verifyTask, origin, VerifyResult{}); err != nil {
		t.Fatalf("scheduleMidFix attempt 3: %v", err)
	}
	if got := countSpawnedByTitlePrefix(t, store, origin.ID, midFixTitlePrefix); got != 2 {
		t.Fatalf("mid-fix children after cap reached = %d, want still 2 (no 3rd redo)", got)
	}
	if got := countSpawnedByAgentID(t, store, origin.ID, escalateAgentID); got != 1 {
		t.Fatalf("escalate children after cap reached = %d, want 1", got)
	}
}

func TestScheduleEscalation_HandsOffToHumanOnceCapReached(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	w := &Worker{
		store: store,
		sink:  &mockEventSink{},
		tieredCfg: config.TieredConfig{
			Escalation: config.TieredEscalationConfig{MaxMidFix: 2, MaxEscalate: 1},
		},
	}
	origin := newTieredPipelineFixture(t, store)
	verifyTask := models.Task{BaseEntity: models.BaseEntity{ID: "verify-1"}, Title: "verify: origin"}

	if err := w.scheduleEscalation(ctx, verifyTask, origin, VerifyResult{Overall: "fail"}); err != nil {
		t.Fatalf("scheduleEscalation attempt 1: %v", err)
	}
	if got := countSpawnedByAgentID(t, store, origin.ID, escalateAgentID); got != 1 {
		t.Fatalf("escalate children after 1st attempt = %d, want 1", got)
	}
	current, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if current.State != models.TaskStateRunning {
		t.Fatalf("origin state after 1st escalation = %s, want still RUNNING", current.State)
	}

	// A second conflict, with the cap already spent, hands off to HUMAN
	// instead of spawning a second escalation.
	if err := w.scheduleEscalation(ctx, verifyTask, *current, VerifyResult{Overall: "fail"}); err != nil {
		t.Fatalf("scheduleEscalation attempt 2: %v", err)
	}
	if got := countSpawnedByAgentID(t, store, origin.ID, escalateAgentID); got != 1 {
		t.Fatalf("escalate children after cap reached = %d, want still 1", got)
	}
	final, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if final.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("origin state after cap reached = %s, want FAILED_REQUIRES_HUMAN", final.State)
	}
}
