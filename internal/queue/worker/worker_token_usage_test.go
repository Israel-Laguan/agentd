package worker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// fakeTokenStore records AddTokenUsage calls for test assertions.
type fakeTokenStore struct {
	mu    sync.Mutex
	calls []tokenUsageCall
}

type tokenUsageCall struct {
	taskID string
	tokens int
}

func (s *fakeTokenStore) AddTokenUsage(_ context.Context, taskID string, tokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, tokenUsageCall{taskID: taskID, tokens: tokens})
	return nil
}

func (s *fakeTokenStore) totalTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, c := range s.calls {
		total += c.tokens
	}
	return total
}

func (s *fakeTokenStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// singleResponseGateway returns a fixed response for every Generate call.
type singleResponseGateway struct {
	resp gateway.AIResponse
	err  error
}

func (g *singleResponseGateway) Generate(_ context.Context, _ gateway.AIRequest) (gateway.AIResponse, error) {
	return g.resp, g.err
}
func (g *singleResponseGateway) GeneratePlan(_ context.Context, _ string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *singleResponseGateway) AnalyzeScope(_ context.Context, _ string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *singleResponseGateway) ClassifyIntent(_ context.Context, _ string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}
func (g *singleResponseGateway) Embed(_ context.Context, _ gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

// newLegacyTokenUsageWorker creates a worker wired for legacy mode with the
// given gateway and token store.
func newLegacyTokenUsageWorker(t *testing.T, gw gateway.AIGateway, ts TokenUsageStore) (*mockAgenticStore, *Worker, models.Task) {
	t.Helper()
	store := &mockAgenticStore{}
	sb := &fakeSuccessExecutor{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		MaxToolIterations: 5,
		TokenStore:        ts,
	})
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-legacy-token"},
		ProjectID:   "proj-1",
		AgentID:     "agent-legacy",
		Title:       "legacy task",
		Description: "run ls",
		State:       models.TaskStateQueued,
	}
	store.profile = models.AgentProfile{
		ID:          "agent-legacy",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: false, // legacy path
	}
	store.project = models.Project{
		BaseEntity:    models.BaseEntity{ID: "proj-1"},
		WorkspacePath: "/tmp/ws",
	}
	store.task = task
	return store, w, task
}

// TestTokenUsage_AgenticPath_WritesSingleTurn verifies that the agentic worker
// calls AddTokenUsage with the response's TokenUsage after each turn.
func TestTokenUsage_AgenticPath_WritesSingleTurn(t *testing.T) {
	t.Parallel()
	ts := &fakeTokenStore{}
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: "[COMPLETED] done", TokenUsage: 15},
	}}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{}}
	store, w, task := newAgenticIntegrationWorker(t, gw, sb, 10)
	w.tokenStore = ts

	w.Process(context.Background(), task)

	if got := ts.totalTokens(); got != 15 {
		t.Errorf("total tokens = %d, want 15", got)
	}
	if got := ts.callCount(); got != 1 {
		t.Errorf("AddTokenUsage calls = %d, want 1", got)
	}
	if ts.calls[0].taskID != task.ID {
		t.Errorf("taskID = %q, want %q", ts.calls[0].taskID, task.ID)
	}
	_ = store
}

func TestTokenUsage_EmitsTokenUsageEvent(t *testing.T) {
	t.Parallel()
	ts := &fakeTokenStore{}
	sink := &mockEventSink{}
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: "[COMPLETED] done", TokenUsage: 15},
	}}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{}}
	_, w, task := newAgenticIntegrationWorker(t, gw, sb, 10)
	w.tokenStore = ts
	w.sink = sink

	w.Process(context.Background(), task)

	var found bool
	for _, ev := range sink.events {
		if ev.Type == models.EventTypeTokenUsage {
			found = true
			if ev.Payload != `{"tokens":15}` {
				t.Errorf("payload = %q, want {\"tokens\":15}", ev.Payload)
			}
			if ev.TaskID.String != task.ID {
				t.Errorf("taskID = %q, want %q", ev.TaskID.String, task.ID)
			}
		}
	}
	if !found {
		t.Fatal("expected TOKEN_USAGE event")
	}
}

// TestTokenUsage_LegacyPath_WritesUsage verifies that the legacy worker records
// token usage from the gateway response via AddTokenUsage.
func TestTokenUsage_LegacyPath_WritesUsage(t *testing.T) {
	t.Parallel()
	ts := &fakeTokenStore{}
	cmd := workerResponse{Command: "echo hi"}
	raw, _ := json.Marshal(cmd)
	gw := &singleResponseGateway{
		resp: gateway.AIResponse{Content: string(raw), TokenUsage: 10},
	}
	_, w, task := newLegacyTokenUsageWorker(t, gw, ts)

	w.Process(context.Background(), task)

	if got := ts.totalTokens(); got != 10 {
		t.Errorf("total tokens = %d, want 10", got)
	}
	if got := ts.callCount(); got != 1 {
		t.Errorf("AddTokenUsage calls = %d, want 1", got)
	}
}

// TestTokenUsage_AgenticPath_Accumulates verifies that multiple agentic turns
// accumulate token usage additively (10 + 5 + 10 = 25).
func TestTokenUsage_AgenticPath_Accumulates(t *testing.T) {
	t.Parallel()
	ts := &fakeTokenStore{}

	toolCall := gateway.ToolCall{
		ID: "call-1", Type: "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo 1"}`},
	}
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: "", ToolCalls: []gateway.ToolCall{toolCall}, TokenUsage: 10},
		{Content: "", ToolCalls: []gateway.ToolCall{toolCall}, TokenUsage: 5},
		{Content: "[COMPLETED] done", TokenUsage: 10},
	}}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"echo 1": {Success: true, ExitCode: 0, Stdout: "1\n"},
	}}
	store, w, task := newAgenticIntegrationWorker(t, gw, sb, 10)
	w.tokenStore = ts

	w.Process(context.Background(), task)

	if got := ts.totalTokens(); got != 25 {
		t.Errorf("total accumulated tokens = %d, want 25", got)
	}
	if got := ts.callCount(); got != 3 {
		t.Errorf("AddTokenUsage calls = %d, want 3", got)
	}
	_ = store
}

// TestTokenUsage_RollingLimitZero_StillRecords verifies that token usage is
// persisted even when TokenUsageHook is nil (rolling_token_limit == 0).
func TestTokenUsage_RollingLimitZero_StillRecords(t *testing.T) {
	t.Parallel()
	ts := &fakeTokenStore{}
	cmd := workerResponse{Command: "echo hi"}
	raw, _ := json.Marshal(cmd)
	gw := &singleResponseGateway{
		resp: gateway.AIResponse{Content: string(raw), TokenUsage: 7},
	}
	// Wire TokenStore but NOT TokenUsageHook (simulates rolling_token_limit=0)
	store := &mockAgenticStore{}
	sb := &fakeSuccessExecutor{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		MaxToolIterations: 5,
		TokenUsageHook:    nil, // explicitly nil — rolling limit not configured
		TokenStore:        ts,
	})
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-no-rolling"},
		ProjectID:   "proj-1",
		AgentID:     "agent-x",
		Title:       "no rolling limit task",
		Description: "echo hi",
		State:       models.TaskStateQueued,
	}
	store.profile = models.AgentProfile{
		ID:          "agent-x",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: false,
	}
	store.project = models.Project{
		BaseEntity:    models.BaseEntity{ID: "proj-1"},
		WorkspacePath: "/tmp/ws",
	}
	store.task = task

	w.Process(context.Background(), task)

	if got := ts.totalTokens(); got != 7 {
		t.Errorf("total tokens = %d, want 7 (rolling limit 0 must not suppress DB write)", got)
	}
}

// TestTokenUsage_APIResponse_IncludesField is a sanity check that models.Task
// has token_usage in its JSON representation — covering the API response requirement.
func TestTokenUsage_APIResponse_IncludesField(t *testing.T) {
	t.Parallel()
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "t1"},
		TokenUsage: 42,
	}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v, ok := m["token_usage"]
	if !ok {
		t.Fatal("token_usage field missing from Task JSON")
	}
	if int(v.(float64)) != 42 {
		t.Errorf("token_usage = %v, want 42", v)
	}
}
