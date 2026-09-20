package worker

import (
	"context"
	"os"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

type storeEventSink struct{ store *testutil.FakeKanbanStore }

func (s *storeEventSink) Emit(ctx context.Context, ev models.Event) error {
	return s.store.AppendEvent(ctx, ev)
}

// plainTextVerifyGateway is a minimal AIGateway double that returns a fixed
// text response with no tool calls, so the agentic loop completes in a
// single turn with that text as the final committed result — exactly the
// shape a real verify step's VerifyResult JSON answer takes.
type plainTextVerifyGateway struct {
	content   string
	callCount int
}

func (g *plainTextVerifyGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.callCount++
	return gateway.AIResponse{Content: g.content, ProviderUsed: "test-provider", ModelUsed: "test-model"}, nil
}
func (g *plainTextVerifyGateway) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *plainTextVerifyGateway) AnalyzeScope(ctx context.Context, userIntent string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *plainTextVerifyGateway) ClassifyIntent(ctx context.Context, userIntent string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}
func (g *plainTextVerifyGateway) Embed(ctx context.Context, req gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}
func (g *plainTextVerifyGateway) ProviderSupportsChatTools(string) bool { return true }

// setUpTieredVerifyFixture builds an origin task (BLOCKED) plus a READY
// verify child wired via SPAWNED_BY (the same relation tryDispatchTieredStep
// resolves), with a ContextPack on disk so injectContextPack succeeds.
func setUpTieredVerifyFixture(t *testing.T, store *testutil.FakeKanbanStore) (origin models.Task, verify models.Task, workspace string) {
	t.Helper()
	ctx := context.Background()
	origin, workspace = setUpTieredVerifyOrigin(t, store)
	writeTieredVerifyPack(t, workspace, origin.ID)
	upsertTieredVerifyProfile(t, ctx, store)
	return origin, spawnTieredVerifyStep(t, ctx, store, origin), workspace
}

func setUpTieredVerifyOrigin(t *testing.T, store *testutil.FakeKanbanStore) (models.Task, string) {
	t.Helper()
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "tiered-verify-fixture",
		Tasks:       []models.DraftTask{{Title: "origin", Description: "complex task"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	originTask, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark origin running: %v", err)
	}
	originTask, err = store.UpdateTaskState(ctx, originTask.ID, originTask.UpdatedAt, models.TaskStateBlocked)
	if err != nil {
		t.Fatalf("block origin: %v", err)
	}

	// FakeKanbanStore.MaterializePlan fixes WorkspacePath to
	// "/tmp/projects/<project name>" with no way to override it after the
	// fact, so create that exact directory for the ContextPack write.
	project, err := store.GetProject(ctx, originTask.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	workspace := project.WorkspacePath
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })
	return *originTask, workspace
}

func writeTieredVerifyPack(t *testing.T, workspace, originID string) {
	t.Helper()
	pack := &ContextPack{
		Version:      ContextPackVersion,
		TaskID:       "context-1",
		ParentTaskID: originID,
		Summary:      "test summary",
		Paths:        []string{"a.go"},
	}
	if err := WriteContextPack(workspace, pack); err != nil {
		t.Fatalf("write context pack: %v", err)
	}
}

func upsertTieredVerifyProfile(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore) {
	t.Helper()
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID:          "tier-verify",
		Provider:    "test-provider",
		Model:       "test-model",
		AgenticMode: true,
	}); err != nil {
		t.Fatalf("upsert tier-verify profile: %v", err)
	}
}

func spawnTieredVerifyStep(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, origin models.Task) models.Task {
	t.Helper()
	verifyDraft := models.Task{
		BaseEntity:  models.BaseEntity{ID: "verify-task"},
		ProjectID:   origin.ProjectID,
		AgentID:     "tier-verify",
		Title:       "verify: origin",
		Description: "run checks",
		State:       models.TaskStateReady,
		Assignee:    models.TaskAssigneeSystem,
	}
	created, err := store.SpawnTieredContinuation(ctx, origin.ID, []models.TieredContinuationTask{{Task: verifyDraft}})
	if err != nil {
		t.Fatalf("spawn verify step: %v", err)
	}
	return created[0]
}

func TestTieredVerify_FailRoutesToMidFix(t *testing.T) {
	store := testutil.NewFakeStore()
	origin, verify, _ := setUpTieredVerifyFixture(t, store)

	gw := &plainTextVerifyGateway{content: `{"results":[{"check":"go test ./...","outcome":"fail","detail":"boom"}],"overall":"fail"}`}
	sb := &mockAgenticSandbox{}
	w := NewWorker(store, gw, sb, nil, &mockEventSink{}, WorkerOptions{
		MaxToolIterations: 10,
		Tiered:            config.TieredConfig{Escalation: config.TieredEscalationConfig{MaxMidFix: 2, MaxEscalate: 1}},
	})

	w.Process(context.Background(), verify)

	if got := countSpawnedByTitlePrefix(t, store, origin.ID, midFixTitlePrefix); got != 1 {
		t.Fatalf("mid-fix children after verify fail = %d, want 1", got)
	}
	current, err := store.GetTask(context.Background(), origin.ID)
	if err != nil {
		t.Fatalf("GetTask origin: %v", err)
	}
	if current.State != models.TaskStateBlocked {
		t.Fatalf("origin state after mid-fix = %s, want still BLOCKED (pipeline continues)", current.State)
	}
}

func TestParseVerifyResult_ValidatesPayload(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{name: "plain JSON", input: `{"results":[{"check":"test","outcome":"pass"}],"overall":"pass"}`, valid: true},
		{name: "markdown fence", input: "```json\n{\"results\":[{\"check\":\"test\",\"outcome\":\"fail\"}],\"overall\":\"fail\"}\n```", valid: true},
		{name: "malformed JSON", input: `{not-json`, valid: false},
		{name: "missing results", input: `{"overall":"pass"}`, valid: false},
		{name: "invalid overall", input: `{"results":[{"check":"test","outcome":"pass"}],"overall":"unknown"}`, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseVerifyResult(test.input)
			if (err == nil) != test.valid {
				t.Fatalf("error = %v, valid = %t", err, test.valid)
			}
		})
	}
}

func TestTieredVerify_ConflictRoutesToEscalation(t *testing.T) {
	store := testutil.NewFakeStore()
	origin, verify, _ := setUpTieredVerifyFixture(t, store)

	gw := &plainTextVerifyGateway{content: `{"results":[{"check":"merge","outcome":"conflict","detail":"merge conflict"}],"overall":"fail"}`}
	sb := &mockAgenticSandbox{}
	w := NewWorker(store, gw, sb, nil, &mockEventSink{}, WorkerOptions{
		MaxToolIterations: 10,
		Tiered:            config.TieredConfig{Escalation: config.TieredEscalationConfig{MaxMidFix: 2, MaxEscalate: 1}},
	})

	w.Process(context.Background(), verify)

	if got := countSpawnedByAgentID(t, store, origin.ID, escalateAgentID); got != 1 {
		t.Fatalf("escalate children after verify conflict = %d, want 1", got)
	}
}

func TestTieredVerify_PassCompletesOrigin(t *testing.T) {
	store := testutil.NewFakeStore()
	origin, verify, _ := setUpTieredVerifyFixture(t, store)

	gw := &plainTextVerifyGateway{content: `{"results":[{"check":"go test ./...","outcome":"pass","detail":""}],"overall":"pass"}`}
	sb := &mockAgenticSandbox{}
	w := NewWorker(store, gw, sb, nil, &storeEventSink{store: store}, WorkerOptions{MaxToolIterations: 10})

	w.Process(context.Background(), verify)

	ctx := context.Background()
	// The verify completion unblocks the origin (BLOCKED → READY).
	// Processing the origin triggers tryResolveTieredOrigin which
	// completes the pipeline.
	w.Process(ctx, origin)

	current, err := store.GetTask(ctx, origin.ID)
	if err != nil {
		t.Fatalf("GetTask origin: %v", err)
	}
	if current.State != models.TaskStateCompleted {
		t.Fatalf("origin state after verify pass = %s, want COMPLETED", current.State)
	}
	events, err := store.ListEventsByTask(ctx, verify.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask: %v", err)
	}
	found := false
	for _, event := range events {
		if string(event.Type) == tieredVerifyOutcomeEvent && event.Payload == string(models.VerifyOutcomePass) {
			found = true
		}
	}
	if !found {
		t.Fatal("verify task has no persisted pass outcome event")
	}
}
