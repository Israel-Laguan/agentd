package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// planningSequenceGateway returns plan JSON on JSONMode requests, then queued responses.
type planningSequenceGateway struct {
	planJSON   string
	responses  []gateway.AIResponse
	callCount  int
	requests   []gateway.AIRequest
	redoBodies map[string]string
}

func (g *planningSequenceGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	if req.JSONMode {
		return gateway.AIResponse{Content: g.planJSON}, nil
	}
	user := ""
	if n := len(req.Messages); n > 0 {
		user = req.Messages[n-1].Content
	}
	if strings.Contains(user, "Repair ONLY step") {
		for id, body := range g.redoBodies {
			if strings.Contains(user, id) {
				return gateway.AIResponse{Content: body}, nil
			}
		}
	}
	if g.callCount >= len(g.responses) {
		return gateway.AIResponse{Content: "done"}, nil
	}
	resp := g.responses[g.callCount]
	g.callCount++
	return resp, nil
}

func (g *planningSequenceGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *planningSequenceGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *planningSequenceGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}
func (g *planningSequenceGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func samplePlanJSON() string {
	p := Plan{Steps: []PlanStep{
		{ID: "analyze", Action: "analyze task", OutputFormat: "text"},
		{ID: "summarize", Action: "summarize", OutputFormat: "text"},
	}}
	b, _ := json.Marshal(p)
	return string(b)
}

func TestAgenticPlanning_ComplexTaskProducesPlan(t *testing.T) {
	t.Parallel()
	gw := &planningSequenceGateway{
		planJSON: samplePlanJSON(),
		responses: []gateway.AIResponse{{
			Content: `<!-- step:analyze -->
ok
<!-- /step:analyze -->
<!-- step:summarize -->
ok2
<!-- /step:summarize -->
`,
		}},
	}
	store := &mockAgenticStore{}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-plan"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Complex",
		Description: strings.Repeat("detail ", 80),
		State:       models.TaskStateQueued,
	}
	store.task = task

	w := NewWorker(store, gw, &mockAgenticSandbox{results: map[string]sandbox.Result{}}, nil, nil, WorkerOptions{
		Planning: config.AgenticPlanningConfig{ComplexityThreshold: 100, MaxRedoPasses: 3},
	})
	w.Process(context.Background(), task)

	if len(gw.requests) < 2 {
		t.Fatalf("expected plan + execute calls, got %d", len(gw.requests))
	}
	if !gw.requests[0].JSONMode || gw.requests[0].Role != gateway.RoleMemory {
		t.Fatalf("first request = JSONMode:%v role:%s, want JSON memory", gw.requests[0].JSONMode, gw.requests[0].Role)
	}
	foundPlan := false
	for _, m := range gw.requests[1].Messages {
		if m.Role == "system" && strings.Contains(m.Content, "WORK PLAN") {
			foundPlan = true
		}
	}
	if !foundPlan {
		t.Fatal("execute request missing injected plan in system prompt")
	}
}

func TestAgenticPlanning_TightTokenBudgetSkipsPlanPhase(t *testing.T) {
	t.Parallel()
	gw := &planningSequenceGateway{
		planJSON:  samplePlanJSON(),
		responses: []gateway.AIResponse{{Content: "done without plan"}},
	}
	store := &mockAgenticStore{}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-tight-budget"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Complex",
		Description: strings.Repeat("detail ", 80),
		State:       models.TaskStateQueued,
	}
	store.task = task

	w := NewWorker(store, gw, &mockAgenticSandbox{results: map[string]sandbox.Result{}}, nil, nil, WorkerOptions{
		Planning:    config.AgenticPlanningConfig{ComplexityThreshold: 100},
		TokenBudget: 2500,
	})
	w.Process(context.Background(), task)

	if len(gw.requests) == 0 {
		t.Fatal("expected at least one gateway call")
	}
	if gw.requests[0].JSONMode {
		t.Fatal("tight token budget should skip plan JSON call")
	}
}

func TestAgenticPlanning_SimpleTaskSkipsPlanning(t *testing.T) {
	t.Parallel()
	gw := &planningSequenceGateway{
		planJSON:  samplePlanJSON(),
		responses: []gateway.AIResponse{{Content: "short answer"}},
	}
	store := &mockAgenticStore{}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-simple"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Hi",
		Description: "short",
		State:       models.TaskStateQueued,
	}
	store.task = task

	w := NewWorker(store, gw, &mockAgenticSandbox{results: map[string]sandbox.Result{}}, nil, nil, WorkerOptions{
		Planning: config.AgenticPlanningConfig{ComplexityThreshold: 1000},
	})
	w.Process(context.Background(), task)

	if len(gw.requests) == 0 {
		t.Fatal("expected at least one gateway call")
	}
	if gw.requests[0].JSONMode {
		t.Fatal("simple task should not start with plan JSON call")
	}
	if gw.requests[0].Role != gateway.RoleWorker {
		t.Fatalf("first role = %s, want worker", gw.requests[0].Role)
	}
}

func TestAgenticPlanning_FailingSectionTargetedRedo(t *testing.T) {
	t.Parallel()
	gw := &planningSequenceGateway{
		planJSON: samplePlanJSON(),
		responses: []gateway.AIResponse{{
			Content: `<!-- step:analyze -->
ok
<!-- /step:analyze -->
`,
		}},
		redoBodies: map[string]string{"summarize": "fixed summary"},
	}
	store := &mockAgenticStore{}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-redo"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Complex redo",
		Description: strings.Repeat("x ", 120),
		State:       models.TaskStateQueued,
	}
	store.task = task

	w := NewWorker(store, gw, &mockAgenticSandbox{results: map[string]sandbox.Result{}}, nil, nil, WorkerOptions{
		Planning: config.AgenticPlanningConfig{ComplexityThreshold: 50, MaxRedoPasses: 3},
	})
	w.Process(context.Background(), task)

	var redoReq *gateway.AIRequest
	for i := range gw.requests {
		if len(gw.requests[i].Messages) == 0 {
			continue
		}
		if strings.Contains(gw.requests[i].Messages[len(gw.requests[i].Messages)-1].Content, "Repair ONLY step") {
			redoReq = &gw.requests[i]
			break
		}
	}
	if redoReq == nil {
		t.Fatal("expected targeted redo gateway call")
	}
	last := redoReq.Messages[len(redoReq.Messages)-1].Content
	if !strings.Contains(last, "summarize") {
		t.Fatalf("redo should target summarize step, got %q", last)
	}
	if !strings.Contains(last, "missing section") {
		t.Fatalf("redo should mention validation error, got %q", last)
	}
	if store.committedResult == nil || !strings.Contains(store.committedResult.Payload, "fixed summary") {
		t.Fatalf("committed payload = %v, want repaired section", store.committedResult)
	}
}

func TestAgenticPlanning_RedoCapAtThreePasses(t *testing.T) {
	t.Parallel()
	gw := &planningSequenceGateway{
		planJSON: `{"steps":[{"id":"only","action":"do","output_format":"text"}]}`,
		responses: []gateway.AIResponse{{
			Content: "<!-- step:only -->\n<!-- /step:only -->\n",
		}},
		redoBodies: map[string]string{"only": ""},
	}
	store := &mockAgenticStore{}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-cap"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Cap",
		Description: strings.Repeat("y ", 100),
		State:       models.TaskStateQueued,
	}
	store.task = task

	w := NewWorker(store, gw, &mockAgenticSandbox{results: map[string]sandbox.Result{}}, nil, nil, WorkerOptions{
		Planning: config.AgenticPlanningConfig{ComplexityThreshold: 50, MaxRedoPasses: 3},
	})
	w.Process(context.Background(), task)

	redoCount := 0
	for _, req := range gw.requests {
		if len(req.Messages) > 0 && strings.Contains(req.Messages[len(req.Messages)-1].Content, "Repair ONLY step") {
			redoCount++
		}
	}
	if redoCount != 3 {
		t.Fatalf("redo gateway calls = %d, want 3", redoCount)
	}
}
