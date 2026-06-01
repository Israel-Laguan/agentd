package agentic

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

type mockAgenticSandbox struct {
	results map[string]sandbox.Result
}

func (s *mockAgenticSandbox) Execute(ctx context.Context, p sandbox.Payload) (sandbox.Result, error) {
	if res, ok := s.results[p.Command]; ok {
		return res, nil
	}
	return sandbox.Result{Success: true}, nil
}

type sequenceGateway struct {
	responses []gateway.AIResponse
	callCount int
}

func (g *sequenceGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if g.callCount >= len(g.responses) {
		return gateway.AIResponse{Content: "no more responses"}, nil
	}
	res := g.responses[g.callCount]
	g.callCount++
	return res, nil
}

func (g *sequenceGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *sequenceGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *sequenceGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

func (g *sequenceGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

type driftThenSequenceGateway struct {
	seq        *sequenceGateway
	driftCalls int
}

func isTopicGuardRequest(req gateway.AIRequest) bool {
	if req.Role == gateway.RoleMemory {
		return true
	}
	for _, m := range req.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "classify whether two messages") {
			return true
		}
	}
	return false
}

func (g *driftThenSequenceGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if isTopicGuardRequest(req) {
		g.driftCalls++
		return gateway.AIResponse{Content: "YES"}, nil
	}
	return g.seq.Generate(ctx, req)
}

func (g *driftThenSequenceGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *driftThenSequenceGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *driftThenSequenceGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

func (g *driftThenSequenceGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

type topicDriftTurnLoopFixture struct {
	e         *Engine
	gw        *driftThenSequenceGateway
	seq       *sequenceGateway
	committed *string
	in        agenticTurnLoopInput
}

type topicDriftHost struct {
	*noopHost
	committed *string
}

func (h *topicDriftHost) CommitTextWithProfile(ctx context.Context, task models.Task, text string, profile *models.AgentProfile) {
	*h.committed = text
}

func (h *topicDriftHost) AssembleAgenticSystemPrompt(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	return []gateway.PromptMessage{{Role: "system", Content: "sys"}}
}

func (h *topicDriftHost) AssembleAgenticSystemPromptWithUserContent(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	userContent string,
) []gateway.PromptMessage {
	return []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: userContent},
	}
}

func (h *topicDriftHost) PrependReviewRejectionFeedback(ctx context.Context, task models.Task, messages []gateway.PromptMessage) ([]gateway.PromptMessage, string) {
	return messages, ""
}

func (h *topicDriftHost) FailHard(ctx context.Context, task models.Task, err error) {}
func (h *topicDriftHost) RunLegacyTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, force bool) {}
func (h *topicDriftHost) RegisterCancel(taskID string, cancel context.CancelFunc) {}
func (h *topicDriftHost) DeregisterCancel(taskID string) {}
func (h *topicDriftHost) HandleGatewayError(ctx context.Context, task models.Task, err error) {}
func (h *topicDriftHost) RunPreTaskElicitation(ctx context.Context, task models.Task, project models.Project) (models.Task, bool, error) {
	return task, false, nil
}
func (h *topicDriftHost) ApplyModelRouting(task models.Task, profile models.AgentProfile, messages []gateway.PromptMessage, tools []gateway.ToolDefinition) models.AgentProfile {
	return profile
}
func (h *topicDriftHost) ApplyTuning(req gateway.AIRequest, task models.Task, profile models.AgentProfile, sessionRecoveryGen int) gateway.AIRequest {
	return req
}
func (h *topicDriftHost) GeneratePlan(ctx context.Context, task models.Task, project models.Project, budget *agentruntime.BudgetGuard) (*agentcontext.Plan, error) {
	return nil, nil
}
func (h *topicDriftHost) InjectPlan(messages []gateway.PromptMessage, plan *agentcontext.Plan) []gateway.PromptMessage {
	return messages
}
func (h *topicDriftHost) RepairOutputWithPlan(ctx context.Context, task models.Task, plan *agentcontext.Plan, content string, budget *agentruntime.BudgetGuard) (string, bool) {
	return content, false
}
func (h *topicDriftHost) GenerateRespecifiedUserTurn(
	ctx context.Context,
	task models.Task,
	plan *agentcontext.Plan,
	failing []agentcontext.PlanStep,
	messages []gateway.PromptMessage,
	cm *agentcontext.ContextManager,
	budget *agentruntime.BudgetGuard,
) (string, error) {
	return "", nil
}
func (h *topicDriftHost) RunSessionStart(ctx context.Context, task models.Task, project models.Project, taskHooks *agenthooks.HookChain) error {
	return nil
}
func (h *topicDriftHost) TryExternalCapabilityRoute(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
	taskCaps *capabilities.Registry,
) (agentruntime.LoopResult, bool, error) {
	return agentruntime.LoopResult{}, false, nil
}
func (h *topicDriftHost) RecordLoopResult(result agentruntime.LoopResult) {}
func (h *topicDriftHost) HandleGoalStalled(ctx context.Context, task models.Task, gt *agentcontext.GoalTracker) error {
	return nil
}

func (h *topicDriftHost) MountAgenticHooks(project models.Project, profile models.AgentProfile) (*agenthooks.HookChain, *capabilities.Registry) {
	return agenthooks.NewHookChain(), capabilities.NewRegistry()
}

func (h *topicDriftHost) AgenticToolsWithExtras(ctx context.Context, executor *agenttools.ToolExecutor, caps *capabilities.Registry) ([]gateway.ToolDefinition, map[string]string) {
	return executor.Definitions(), nil
}

func (h *topicDriftHost) FilterAgenticTools(tools []gateway.ToolDefinition, toolToAdapter map[string]string, task models.Task, profile models.AgentProfile) ([]gateway.ToolDefinition, map[string]string) {
	return tools, toolToAdapter
}

func (h *topicDriftHost) ShouldPlanWithBudget(task models.Task, budget *agentruntime.BudgetGuard) bool {
	return false
}

func (h *topicDriftHost) DispatchToolWithHooks(
	ctx context.Context,
	sessionID, projectID, turnID string,
	taskUpdatedAt time.Time,
	call gateway.ToolCall,
	toolToAdapter map[string]string,
	toolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain,
	taskCaps *capabilities.Registry,
	providerName string,
) (agenttools.ToolResult, bool) {
	output := toolExecutor.Execute(ctx, call)
	return agenttools.SuccessResult(call.ID, output, 0), false
}

func newTopicDriftTurnLoopEngine(
	t *testing.T,
) (*Engine, *driftThenSequenceGateway, *sequenceGateway, *string, models.Task, models.KanbanStore) {
	t.Helper()
	now := time.Now().UTC()
	committed := ""
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: now},
		ProjectID:  "project-1",
		AgentID:    "agent-1",
		Title:      "CSS styling",
	}
	store := testutil.NewFakeStore()
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now},
		TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "database migrations",
	})
	seq := &sequenceGateway{responses: []gateway.AIResponse{
		{
			Content: "running a command",
			ToolCalls: []gateway.ToolCall{{
				ID: "call_1", Type: "function",
				Function: gateway.ToolCallFunction{Name: "bash", Arguments: `echo ok`},
			}},
		},
		{Content: "task completed after topic drift rewind"},
	}}
	gw := &driftThenSequenceGateway{seq: seq}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"echo ok": {Success: true, ExitCode: 0, Stdout: "ok\n"},
	}}
	host := &topicDriftHost{noopHost: &noopHost{}, committed: &committed}
	tg := agentruntime.NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true})
	e := NewEngine(Config{
		Store:      store,
		Gateway:    gw,
		Sandbox:    sb,
		TopicGuard: tg,
	}, host)
	return e, gw, seq, &committed, task, store
}

func buildTopicDriftAgenticTurnLoopInput(
	t *testing.T, e *Engine, task models.Task, messages *[]gateway.PromptMessage, store models.KanbanStore,
) agenticTurnLoopInput {
	t.Helper()
	project := models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	sessionMgr := NewSessionManager(task.ID, "CSS styling", e.config.CheckpointStore)
	cmCfg := config.AgenticContextConfig{
		RollingThresholdTurns: 10,
		KeepRecentTurns:       5,
		AnchorBudget:          1000,
		WorkingBudget:         1000,
		CompressedBudget:      1000,
	}
	cm := agentcontext.NewContextManager(cmCfg, e.config.Gateway, task.AgentID, task.ID)
	goalTracker := agentcontext.NewGoalTracker(task.ID, task.ProjectID, agentcontext.WithCriteriaStore(store))
	contextBudget := cm.TotalBudget()
	ctxBudgetGuard := agentruntime.NewContextBudgetGuard(contextBudget, 0.9)
	taskToolExecutor := agenttools.NewToolExecutor(
		e.config.Sandbox,
		project.WorkspacePath,
		agenttools.BuildSandboxEnv(nil, nil),
		0,
	)
	taskHooks, taskCaps := e.host.MountAgenticHooks(project, profile)
	tools, toolToAdapter := e.host.AgenticToolsWithExtras(context.Background(), taskToolExecutor, taskCaps)
	return agenticTurnLoopInput{
		ctx:              context.Background(),
		task:             task,
		project:          project,
		profile:          profile,
		messages:         messages,
		tools:            tools,
		toolToAdapter:    toolToAdapter,
		taskToolExecutor: taskToolExecutor,
		iterationGuard:   agentruntime.NewIterationGuard(10),
		budgetGuard:      agentruntime.NewBudgetGuard(nil, task.ID),
		deadlineGuard:    agentruntime.NewDeadlineGuard(context.Background()),
		ctxBudgetGuard:   ctxBudgetGuard,
		cm:               cm,
		goalTracker:      goalTracker,
		taskHooks:        taskHooks,
		taskCaps:         taskCaps,
		toolTracker:      agenttools.NewToolFailureTracker(0),
		sessionMgr:       sessionMgr,
	}
}

func setupTopicDriftTurnLoopRewindFixture(t *testing.T) topicDriftTurnLoopFixture {
	t.Helper()
	e, gw, seq, committed, task, store := newTopicDriftTurnLoopEngine(t)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "CSS styling"},
		{Role: "assistant", Content: "styled"},
	}
	return topicDriftTurnLoopFixture{
		e: e, gw: gw, seq: seq, committed: committed,
		in: buildTopicDriftAgenticTurnLoopInput(t, e, task, &messages, store),
	}
}

func assertTopicDriftTurnLoopContinuesAfterRewind(
	t *testing.T, f topicDriftTurnLoopFixture, result agentruntime.LoopResult, reported bool,
) {
	t.Helper()
	if !reported {
		t.Fatal("expected loop to report a result after continuing past topic drift")
	}
	if result.Status != agentruntime.LoopSuccessfulCompletion {
		t.Fatalf("result status = %v, want successful completion", result.Status)
	}
	if f.gw.driftCalls < 1 {
		t.Fatal("expected topic drift guard to call gateway")
	}
	if f.seq.callCount < 2 {
		t.Fatalf("expected at least 2 agentic gateway calls after drift rewind, got %d", f.seq.callCount)
	}
	for _, m := range *f.in.messages {
		if m.Role == "assistant" && m.Content == "styled" {
			t.Fatal("assistant history from before drift should be cleared after rewind")
		}
	}
	if *f.committed == "" {
		t.Fatal("expected committed text after loop completion")
	}
}

func TestGuardTopicDrift_SkipsFirstTurn(t *testing.T) {
	gw := &driftThenSequenceGateway{seq: &sequenceGateway{}, driftCalls: 0}
	tg := agentruntime.NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true})
	e := &Engine{
		config: Config{
			Store:      testutil.NewFakeStore(),
			Gateway:    gw,
			TopicGuard: tg,
		},
	}
	sm := NewSessionManager("task-1", "CSS styling", nil)

	messages := []gateway.PromptMessage{{Role: "user", Content: "CSS styling"}}
	err := e.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 0, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if gw.driftCalls != 0 {
		t.Fatalf("expected no drift gateway calls on turn 0, got %d", gw.driftCalls)
	}
}

func TestGuardTopicDrift_NoNewComment_SkipsDriftCall(t *testing.T) {
	gw := &driftThenSequenceGateway{seq: &sequenceGateway{}, driftCalls: 0}
	tg := agentruntime.NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true})
	e := &Engine{
		config: Config{
			Store:      testutil.NewFakeStore(),
			Gateway:    gw,
			TopicGuard: tg,
		},
	}
	sm := NewSessionManager("task-1", "CSS styling", nil)
	messages := []gateway.PromptMessage{{Role: "user", Content: "CSS styling"}}

	err := e.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if gw.driftCalls != 0 {
		t.Fatalf("expected no gateway calls, got %d", gw.driftCalls)
	}
}

func TestGuardTopicDrift_DriftBeforeCompression(t *testing.T) {
	gw := &driftThenSequenceGateway{seq: &sequenceGateway{}, driftCalls: 0}
	tg := agentruntime.NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true})
	store := testutil.NewFakeStore()
	now := time.Now().UTC()
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now},
		TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "database migrations",
	})

	committed := ""
	host := &topicDriftHost{noopHost: &noopHost{}, committed: &committed}
	e := &Engine{
		config: Config{
			Store:      store,
			Gateway:    gw,
			TopicGuard: tg,
		},
		host: host,
	}
	sm := NewSessionManager("task-1", "CSS styling", nil)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "CSS styling"},
		{Role: "assistant", Content: "styled"},
	}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}

	err := e.guardTopicDrift(context.Background(), task, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if !errors.Is(err, errTopicDriftReset) {
		t.Fatalf("guardTopicDrift err = %v, want errTopicDriftReset", err)
	}
	for _, m := range messages {
		if m.Role == "assistant" {
			t.Fatal("assistant history should be cleared after drift")
		}
	}
}

func TestRunAgenticTurnLoop_TopicDriftErrWithRewindContinues(t *testing.T) {
	f := setupTopicDriftTurnLoopRewindFixture(t)
	result, reported := f.e.runAgenticTurnLoop(f.in)
	assertTopicDriftTurnLoopContinuesAfterRewind(t, f, result, reported)
}

func TestGuardTopicDrift_RelatedFollowUp_NoReset(t *testing.T) {
	// mock drift gateway returns "NO"
	seq := &sequenceGateway{responses: []gateway.AIResponse{{Content: "NO"}}}
	tg := agentruntime.NewTopicGuard(seq, config.TopicGuardConfig{Enabled: true})
	store := testutil.NewFakeStore()
	now := time.Now().UTC()
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now},
		TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "now add tests for the migration",
	})
	e := &Engine{
		config: Config{
			Store:      store,
			Gateway:    seq,
			TopicGuard: tg,
		},
	}
	sm := NewSessionManager("task-1", "database migrations", nil)
	messages := []gateway.PromptMessage{{Role: "user", Content: "database migrations"}}

	err := e.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if sm.TopicAnchor() != "now add tests for the migration" {
		t.Fatalf("topic anchor = %q, want updated follow-up", sm.TopicAnchor())
	}
}
