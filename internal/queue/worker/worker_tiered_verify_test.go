package worker

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// storeEventSink persists emitted events into the fake store so the tiered
// origin can read verify verdicts back the way it does in production.
type storeEventSink struct {
	store *testutil.FakeKanbanStore
}

func (s *storeEventSink) Emit(ctx context.Context, ev models.Event) error {
	return s.store.AppendEvent(ctx, ev)
}

// tieredFixture is a persisted origin task with its four tiered step children.
type tieredFixture struct {
	ctx    context.Context
	store  *testutil.FakeKanbanStore
	worker *Worker
	origin models.Task
	steps  []models.Task
}

func (f *tieredFixture) step(t *testing.T, kind TieredStepKind) models.Task {
	t.Helper()
	for _, s := range f.steps {
		if tieredStepProfiles[s.AgentID] == kind {
			return s
		}
	}
	t.Fatalf("no %s step in fixture", kind)
	return models.Task{}
}

func (f *tieredFixture) reload(t *testing.T, id string) models.Task {
	t.Helper()
	task, err := f.store.GetTask(f.ctx, id)
	if err != nil {
		t.Fatalf("reload %s: %v", id, err)
	}
	return *task
}

// tieredChildren returns the origin's tiered step children.
func (f *tieredFixture) tieredChildren(t *testing.T) []models.Task {
	t.Helper()
	steps, err := f.worker.tieredStepChildren(f.ctx, f.origin)
	if err != nil {
		t.Fatalf("list tiered steps: %v", err)
	}
	return steps
}

func newTieredFixture(t *testing.T) *tieredFixture {
	t.Helper()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "tiered-proj",
		Tasks:       []models.DraftTask{{Title: "Complex", Description: "do the complex thing"}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	origin := tasks[0]

	children, _ := SplitIntoTieredDAG(origin, time.Now().UTC())
	dag := make([]models.TieredDAGTask, len(children))
	for i, child := range children {
		var dependsOn string
		if i > 0 {
			dependsOn = children[i-1].ID
		}
		dag[i] = models.TieredDAGTask{Task: child, DependsOnID: dependsOn}
	}
	steps, err := store.PersistTieredDAG(ctx, origin.ID, origin.UpdatedAt, dag)
	if err != nil {
		t.Fatalf("persist tiered dag: %v", err)
	}

	w := &Worker{store: store, sink: &storeEventSink{store: store}}
	return &tieredFixture{
		ctx:    ctx,
		store:  store,
		worker: w,
		origin: *mustGet(t, store, ctx, origin.ID),
		steps:  steps,
	}
}

func mustGet(t *testing.T, store *testutil.FakeKanbanStore, ctx context.Context, id string) *models.Task {
	t.Helper()
	task, err := store.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("get task %s: %v", id, err)
	}
	return task
}

// completeSteps marks the given steps COMPLETED so the origin unblocks.
func (f *tieredFixture) completeSteps(t *testing.T, tasks ...models.Task) {
	t.Helper()
	for _, task := range tasks {
		current := f.reload(t, task.ID)
		if _, err := f.store.UpdateTaskResult(f.ctx, current.ID, current.UpdatedAt,
			models.TaskResult{Success: true}); err != nil {
			t.Fatalf("complete step %s: %v", task.ID, err)
		}
	}
}

func TestParseVerifyResult(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		output      string
		wantOverall string
		wantErr     bool
	}{
		{
			name:        "plain json",
			output:      `{"results":[{"check":"go test","outcome":"pass"}],"overall":"pass"}`,
			wantOverall: "pass",
		},
		{
			name:        "worker result preamble",
			output:      "exit=0 duration=1.2s\n{\"results\":[],\"overall\":\"fail\"}",
			wantOverall: "fail",
		},
		{
			name:        "fenced json",
			output:      "Here you go:\n```json\n{\"results\":[],\"overall\":\"pass\"}\n```\n",
			wantOverall: "pass",
		},
		{
			name:        "unlabelled fence",
			output:      "```\n{\"results\":[],\"overall\":\"fail\"}\n```",
			wantOverall: "fail",
		},
		{
			name:        "braces inside strings",
			output:      `{"results":[{"check":"grep {x}","outcome":"fail","detail":"}"}],"overall":"fail"}`,
			wantOverall: "fail",
		},
		{name: "no json", output: "the checks all passed", wantErr: true},
		{name: "empty object", output: "{}", wantErr: true},
		{name: "malformed json", output: `{"overall": }`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseVerifyResult(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseVerifyResult(%q) = %+v, want error", tt.output, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVerifyResult(%q) error = %v", tt.output, err)
			}
			if got.Overall != tt.wantOverall {
				t.Errorf("overall = %q, want %q", got.Overall, tt.wantOverall)
			}
		})
	}
}

func TestReadVerifyResult_UsesLatestResultEvent(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	verify := f.step(t, TieredStepVerify)

	// An earlier result plus the one that matters: the latest must win.
	for _, payload := range []string{
		`exit=0 duration=1s` + "\n" + `{"results":[],"overall":"pass"}`,
		`exit=0 duration=2s` + "\n" + `{"results":[{"check":"go test","outcome":"fail","detail":"boom"}],"overall":"fail"}`,
	} {
		if err := f.store.AppendEvent(f.ctx, models.Event{
			ProjectID: verify.ProjectID,
			TaskID:    nullString(verify.ID),
			Type:      models.EventTypeResult,
			Payload:   payload,
		}); err != nil {
			t.Fatalf("append result event: %v", err)
		}
	}

	got, err := f.worker.readVerifyResult(f.ctx, verify)
	if err != nil {
		t.Fatalf("readVerifyResult: %v", err)
	}
	if got.Overall != "fail" {
		t.Errorf("overall = %q, want %q (latest RESULT event)", got.Overall, "fail")
	}
	if ClassifyVerifyOutcome(got) != models.VerifyOutcomeFail {
		t.Errorf("classified = %v, want fail", ClassifyVerifyOutcome(got))
	}
}

func TestReadVerifyResult_NoResultEvent(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	if _, err := f.worker.readVerifyResult(f.ctx, f.step(t, TieredStepVerify)); err == nil {
		t.Fatal("readVerifyResult with no RESULT event = nil error, want error")
	}
}

func TestHandleVerifyOutcome_PassSchedulesNothing(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	verify := f.step(t, TieredStepVerify)

	err := f.worker.handleVerifyOutcome(f.ctx, verify, models.VerifyOutcomePass,
		VerifyResult{Overall: "pass"}, f.origin)
	if err != nil {
		t.Fatalf("handleVerifyOutcome(pass): %v", err)
	}
	if got := len(f.tieredChildren(t)); got != 4 {
		t.Errorf("steps after pass = %d, want 4 (no redo scheduled)", got)
	}
}

func TestHandleVerifyOutcome_FailSchedulesMidFixPair(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	verify := f.step(t, TieredStepVerify)
	// The verify step's own success unblocks the origin before classification.
	f.completeSteps(t, f.steps...)

	evidence := VerifyResult{
		Overall: "fail",
		Results: []CheckResult{{Check: "go test ./...", Outcome: "fail", Detail: "TestFoo failed"}},
	}
	if err := f.worker.handleVerifyOutcome(f.ctx, verify, models.VerifyOutcomeFail, evidence, f.origin); err != nil {
		t.Fatalf("handleVerifyOutcome(fail): %v", err)
	}

	steps := f.tieredChildren(t)
	if len(steps) != 6 {
		t.Fatalf("steps after mid fix = %d, want 6 (execute + verify appended)", len(steps))
	}
	if got := countTieredSteps(steps, TieredStepExecute); got != 2 {
		t.Errorf("execute steps = %d, want 2", got)
	}
	if got := countTieredSteps(steps, TieredStepVerify); got != 2 {
		t.Errorf("verify steps = %d, want 2", got)
	}
	// The redo must re-block the origin, otherwise it would resolve as done
	// while the ladder is still running.
	if state := f.reload(t, f.origin.ID).State; state != models.TaskStateBlocked {
		t.Errorf("origin state = %s, want %s", state, models.TaskStateBlocked)
	}
	// The failing evidence has to reach the redo so the next attempt is informed.
	var redo *models.Task
	for i, s := range steps {
		if tieredStepProfiles[s.AgentID] == TieredStepExecute && s.ID != f.step(t, TieredStepExecute).ID {
			redo = &steps[i]
		}
	}
	if redo == nil {
		t.Fatal("no mid-fix execute step found")
	}
	if !strings.Contains(redo.Description, "TestFoo failed") {
		t.Errorf("redo description lost the verify evidence: %q", redo.Description)
	}
}

func TestScheduleMidFix_CapRoutesToEscalation(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	f.completeSteps(t, f.steps...)

	// Burn both mid-fix passes.
	for i := 0; i < maxMidFixPasses; i++ {
		if err := f.worker.scheduleMidFix(f.ctx, f.origin, VerifyResult{Overall: "fail"}); err != nil {
			t.Fatalf("scheduleMidFix pass %d: %v", i+1, err)
		}
		f.completeSteps(t, f.tieredChildren(t)...)
	}
	if got := countTieredSteps(f.tieredChildren(t), TieredStepVerify); got != maxMidFixPasses+1 {
		t.Fatalf("verify steps after %d mid fixes = %d, want %d",
			maxMidFixPasses, got, maxMidFixPasses+1)
	}

	// The next failure must escalate rather than schedule a third redo.
	if err := f.worker.scheduleMidFix(f.ctx, f.origin, VerifyResult{Overall: "fail"}); err != nil {
		t.Fatalf("scheduleMidFix past cap: %v", err)
	}
	steps := f.tieredChildren(t)
	if got := countTieredSteps(steps, TieredStepEscalate); got != 1 {
		t.Errorf("escalate steps = %d, want 1", got)
	}
	if got := countTieredSteps(steps, TieredStepExecute); got != 1+maxMidFixPasses {
		t.Errorf("execute steps = %d, want %d (no extra redo past the cap)",
			got, 1+maxMidFixPasses)
	}
}

func TestScheduleEscalation_CapHandsOffToHuman(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	f.completeSteps(t, f.steps...)

	if err := f.worker.scheduleEscalation(f.ctx, f.origin, VerifyResult{Overall: "fail"}); err != nil {
		t.Fatalf("first escalation: %v", err)
	}
	f.completeSteps(t, f.tieredChildren(t)...)

	if err := f.worker.scheduleEscalation(f.ctx, f.origin, VerifyResult{Overall: "fail"}); err != nil {
		t.Fatalf("second escalation: %v", err)
	}
	if got := countTieredSteps(f.tieredChildren(t), TieredStepEscalate); got != maxEscalations {
		t.Errorf("escalate steps = %d, want %d (cap enforced)", got, maxEscalations)
	}
	if state := f.reload(t, f.origin.ID).State; state != models.TaskStateFailedRequiresHuman {
		t.Errorf("origin state = %s, want %s", state, models.TaskStateFailedRequiresHuman)
	}
}

func TestTryResolveTieredOrigin_NotATieredOrigin(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "plain",
		Tasks:       []models.DraftTask{{Title: "Plain", Description: "no dag"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	w := &Worker{store: store}
	if w.tryResolveTieredOrigin(ctx, tasks[0]) {
		t.Error("tryResolveTieredOrigin(plain task) = true, want false")
	}
}

func TestTryResolveTieredOrigin_PassCompletesOrigin(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	verify := f.step(t, TieredStepVerify)
	f.completeSteps(t, f.steps...)
	f.worker.Emit(f.ctx, verify, tieredVerifyOutcomeEvent, string(models.VerifyOutcomePass))

	origin := f.reload(t, f.origin.ID)
	if !f.worker.tryResolveTieredOrigin(f.ctx, origin) {
		t.Fatal("tryResolveTieredOrigin = false, want true")
	}
	if state := f.reload(t, f.origin.ID).State; state != models.TaskStateCompleted {
		t.Errorf("origin state = %s, want %s", state, models.TaskStateCompleted)
	}
}

func TestTryResolveTieredOrigin_PendingStepReblocks(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	// Only the first two steps are done; execute and verify are outstanding.
	f.completeSteps(t, f.step(t, TieredStepContext), f.step(t, TieredStepDecision))

	origin := f.reload(t, f.origin.ID)
	if _, err := f.store.UpdateTaskState(f.ctx, origin.ID, origin.UpdatedAt, models.TaskStateReady); err != nil {
		t.Fatalf("force origin ready: %v", err)
	}
	origin = f.reload(t, f.origin.ID)

	if !f.worker.tryResolveTieredOrigin(f.ctx, origin) {
		t.Fatal("tryResolveTieredOrigin = false, want true")
	}
	if state := f.reload(t, f.origin.ID).State; state != models.TaskStateBlocked {
		t.Errorf("origin state = %s, want %s", state, models.TaskStateBlocked)
	}
}

func TestTryResolveTieredOrigin_FailedVerifyLeavesLadderInCharge(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	verify := f.step(t, TieredStepVerify)
	f.completeSteps(t, f.steps...)
	f.worker.Emit(f.ctx, verify, tieredVerifyOutcomeEvent, string(models.VerifyOutcomeFail))

	origin := f.reload(t, f.origin.ID)
	if !f.worker.tryResolveTieredOrigin(f.ctx, origin) {
		t.Fatal("tryResolveTieredOrigin = false, want true")
	}
	// A failed verdict must never complete the origin: the ladder owns it.
	state := f.reload(t, f.origin.ID).State
	if state == models.TaskStateCompleted {
		t.Fatal("origin completed despite a failing verify verdict")
	}
	if state != models.TaskStateBlocked {
		t.Errorf("origin state = %s, want %s", state, models.TaskStateBlocked)
	}
}

func TestTryResolveTieredOrigin_PreventsResplit(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	verify := f.step(t, TieredStepVerify)
	f.completeSteps(t, f.steps...)
	f.worker.Emit(f.ctx, verify, tieredVerifyOutcomeEvent, string(models.VerifyOutcomePass))

	// An origin whose pipeline already ran must be claimed by the resolver so
	// the split gate never sees it again.
	origin := f.reload(t, f.origin.ID)
	if !f.worker.tryResolveTieredOrigin(f.ctx, origin) {
		t.Fatal("tryResolveTieredOrigin = false; origin would be split a second time")
	}
	if got := len(f.tieredChildren(t)); got != 4 {
		t.Errorf("steps after resolve = %d, want 4 (no re-split)", got)
	}
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
