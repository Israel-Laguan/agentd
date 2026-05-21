package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// planningSequenceGateway returns plan JSON on JSONMode requests, then queued responses.
type planningSequenceGateway struct {
	planJSON   string
	responses  []gateway.AIResponse
	callCount  int
	requests   []gateway.AIRequest
	redoBodies map[string]string
}

func (g *planningSequenceGateway) isPlanRequest(req gateway.AIRequest) bool {
	if !req.JSONMode {
		return false
	}
	for _, m := range req.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "task planner") {
			return true
		}
	}
	return false
}

func (g *planningSequenceGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	if g.isPlanRequest(req) {
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
	if strings.HasPrefix(user, "Revise the user task prompt") {
		return gateway.AIResponse{Content: "revised task with clearer acceptance criteria"}, nil
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
		Description: strings.Repeat("detail ", 80) + "\n- must complete all plan steps\nAcceptance: committed output matches plan.",
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
	if store.committedResult == nil {
		t.Fatal("expected committed result")
	}
	if strings.Contains(store.committedResult.Payload, "<!-- step:") {
		t.Fatalf("committed payload should not contain step markers: %q", store.committedResult.Payload)
	}
	if !strings.Contains(store.committedResult.Payload, "ok") || !strings.Contains(store.committedResult.Payload, "ok2") {
		t.Fatalf("committed payload = %q, want step bodies", store.committedResult.Payload)
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
		Description: strings.Repeat("detail ", 80) + "\n- must complete all plan steps\nAcceptance: committed output matches plan.",
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
		Description: testutil.AgenticTestTaskDescription(),
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
		Description: strings.Repeat("x ", 120) + "\n- must complete all plan steps\nAcceptance: committed output matches plan.",
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
	if strings.Contains(store.committedResult.Payload, "<!-- step:") {
		t.Fatalf("committed payload should not contain step markers: %q", store.committedResult.Payload)
	}
}

func TestAgenticPlanning_RespecAfterRedoExhausted(t *testing.T) {
	t.Parallel()
	gw := &planningSequenceGateway{
		planJSON: `{"steps":[{"id":"only","action":"do","output_format":"text"}]}`,
		responses: []gateway.AIResponse{
			{Content: "<!-- step:only -->\n<!-- /step:only -->\n"},
			{Content: "<!-- step:only -->\n<!-- /step:only -->\n"},
			{Content: "<!-- step:only -->\nfixed\n<!-- /step:only -->\n"},
		},
		redoBodies: map[string]string{"only": ""},
	}
	store := &mockAgenticStore{}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-respec"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Respec",
		Description: strings.Repeat("z ", 100) + "\n- must complete all plan steps\nAcceptance: committed output matches plan.",
		State:       models.TaskStateQueued,
	}
	store.task = task

	w := NewWorker(store, gw, &mockAgenticSandbox{results: map[string]sandbox.Result{}}, nil, nil, WorkerOptions{
		Planning: config.AgenticPlanningConfig{ComplexityThreshold: 50, MaxRedoPasses: 3},
	})
	w.Process(context.Background(), task)

	var respecReq bool
	for _, req := range gw.requests {
		if len(req.Messages) == 0 {
			continue
		}
		last := req.Messages[len(req.Messages)-1].Content
		if strings.Contains(last, "Revise the user task prompt") {
			respecReq = true
		}
	}
	if !respecReq {
		t.Fatal("expected gateway respec request after redo exhausted")
	}
	if w.checkpointStore == nil {
		t.Fatal("expected checkpoint store on worker")
	}
	if store.committedResult == nil {
		t.Fatal("expected committed result after loop rerun")
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
		Description: strings.Repeat("y ", 100) + "\n- must complete all plan steps\nAcceptance: committed output matches plan.",
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
	if redoCount < 3 {
		t.Fatalf("redo gateway calls = %d, want at least 3", redoCount)
	}
	if store.committedResult == nil {
		t.Fatal("expected committed result")
	}
	if strings.Contains(store.committedResult.Payload, "<!-- step:") {
		t.Fatalf("committed payload should not contain step markers: %q", store.committedResult.Payload)
	}
}

func TestAgenticPlanning_PrePlanRestoreAfterRedoExhausted(t *testing.T) {
	t.Parallel()
	gw := &planningSequenceGateway{
		planJSON: `{"steps":[{"id":"only","action":"do","output_format":"text"}]}`,
		responses: []gateway.AIResponse{
			{Content: "<!-- step:only -->\n<!-- /step:only -->\n"},
			{Content: "<!-- step:only -->\nrecovered\n<!-- /step:only -->\n"},
		},
		redoBodies: map[string]string{"only": ""},
	}
	store := &mockAgenticStore{}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-restore"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Restore",
		Description: strings.Repeat("w ", 100) + "\n- must complete all plan steps\nAcceptance: committed output matches plan.",
		State:       models.TaskStateQueued,
	}
	store.task = task

	tuner := planning.NewParameterTuner(config.HealingConfig{
		Enabled:        true,
		Steps:          []string{planning.HealingStepUpgradeModel},
		UpgradeModel:   "gpt-4o",
		MaxAdjustments: 3,
	})
	w := NewWorker(store, gw, &mockAgenticSandbox{results: map[string]sandbox.Result{}}, nil, nil, WorkerOptions{
		Planning: config.AgenticPlanningConfig{ComplexityThreshold: 50, MaxRedoPasses: 0},
		Tuner:    tuner,
	})
	w.Process(context.Background(), task)

	var postRestoreModel string
	for _, req := range gw.requests {
		if len(req.Messages) == 0 || req.JSONMode {
			continue
		}
		last := req.Messages[len(req.Messages)-1].Content
		if strings.Contains(last, "Repair ONLY step") || strings.HasPrefix(last, "Revise the user task prompt") {
			continue
		}
		if req.Model == "gpt-4o" {
			postRestoreModel = req.Model
		}
	}
	if postRestoreModel != "gpt-4o" {
		t.Fatalf("post-restore agentic model = %q, want gpt-4o", postRestoreModel)
	}
	if store.committedResult == nil || !strings.Contains(store.committedResult.Payload, "recovered") {
		t.Fatalf("committed payload = %v, want recovered output", store.committedResult)
	}
}
