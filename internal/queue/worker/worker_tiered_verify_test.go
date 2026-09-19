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

// setUpTieredVerifyFixture builds an origin task (RUNNING) plus a READY
// verify child wired via SPAWNED_BY (the same relation tryDispatchTieredStep
// resolves), with a ContextPack on disk so injectContextPack succeeds.
func setUpTieredVerifyFixture(t *testing.T, store *testutil.FakeKanbanStore) (origin models.Task, verify models.Task, workspace string) {
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
	origin = *originTask

	// FakeKanbanStore.MaterializePlan fixes WorkspacePath to
	// "/tmp/projects/<project name>" with no way to override it after the
	// fact, so create that exact directory for the ContextPack write.
	project, err := store.GetProject(ctx, origin.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	workspace = project.WorkspacePath
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })

	pack := &ContextPack{
		Version:      ContextPackVersion,
		TaskID:       "context-1",
		ParentTaskID: origin.ID,
		Summary:      "test summary",
		Paths:        []string{"a.go"},
	}
	if err := WriteContextPack(workspace, pack); err != nil {
		t.Fatalf("write context pack: %v", err)
	}

	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID:          "tier-verify",
		Provider:    "test-provider",
		Model:       "test-model",
		AgenticMode: true,
	}); err != nil {
		t.Fatalf("upsert tier-verify profile: %v", err)
	}

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
	verify = created[0]
	return origin, verify, workspace
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
	if current.State != models.TaskStateRunning {
		t.Fatalf("origin state after mid-fix = %s, want still RUNNING (pipeline continues)", current.State)
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
	w := NewWorker(store, gw, sb, nil, &mockEventSink{}, WorkerOptions{MaxToolIterations: 10})

	w.Process(context.Background(), verify)

	current, err := store.GetTask(context.Background(), origin.ID)
	if err != nil {
		t.Fatalf("GetTask origin: %v", err)
	}
	if current.State != models.TaskStateCompleted {
		t.Fatalf("origin state after verify pass = %s, want COMPLETED", current.State)
	}
}
