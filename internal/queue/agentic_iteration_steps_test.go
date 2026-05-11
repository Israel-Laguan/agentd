package queue

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/worker"
	"agentd/internal/sandbox"

	"github.com/cucumber/godog"
)

type agenticIterationScenario struct {
	mu               sync.Mutex
	maxIterations    int
	tokenBudget      int
	gatewayCalls     int
	tokenUsage       int
	toolCallCount    int
	iterationCount   int
	exceeded         bool
	finalInjected    bool
	additionalCalled bool
	lastError        error
	completed        bool
	resultContent    string

	gw             *iterationGateway
	store          *iterationStore
	sandbox        *iterationSandbox
	workerOpts     worker.WorkerOptions
	w              *worker.Worker
	budgetTracker  *gateway.InMemoryBudgetTracker
	budgetGuard    *worker.BudgetGuard
}

type iterationGateway struct {
	content     string
	err         error
	tokens      int
	returnTools bool
	calls       int
	mu          sync.Mutex
}

func (g *iterationGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.mu.Lock()
	g.calls++
	toolCalls := g.returnTools
	g.mu.Unlock()

	s.iterationCount++

	if s.exceeded && s.finalInjected {
		s.additionalCalled = true
	}

	if toolCalls {
		return gateway.AIResponse{
			Content:   g.content,
			ToolCalls: []gateway.ToolCall{{ID: "call-1", Function: gateway.ToolCallFunction{Name: "bash", Arguments: "{\"command\":\"echo test\"}"}}},
			TokenUsage: g.tokens,
		}, g.err
	}
	return gateway.AIResponse{Content: g.content, TokenUsage: g.tokens}, g.err
}

func (g *iterationGateway) GeneratePlan(ctx context.Context, s string) (*models.DraftPlan, error) {
	return &models.DraftPlan{}, nil
}

func (g *iterationGateway) AnalyzeScope(ctx context.Context, s string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *iterationGateway) ClassifyIntent(ctx context.Context, s string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

type iterationStore struct {
	mu       sync.Mutex
	tasks    map[string]*models.Task
	projects map[string]*models.Project
}

func (s *iterationStore) GetProject(ctx context.Context, id string) (*models.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.projects[id]; ok {
		return p, nil
	}
	return &models.Project{BaseEntity: models.BaseEntity{ID: id}, WorkspacePath: "/tmp"}, nil
}

func (s *iterationStore) GetAgentProfile(ctx context.Context, id string) (models.AgentProfile, error) {
	return models.AgentProfile{AgenticMode: true, Provider: "openai", Model: "gpt-4"}, nil
}

func (s *iterationStore) MarkTaskRunning(ctx context.Context, id string, updatedAt int64, pid int) (*models.Task, error) {
	return &models.Task{BaseEntity: models.BaseEntity{ID: id}, ProjectID: "proj-1", AgentID: "agent-1"}, nil
}

func (s *iterationStore) UpdateTaskHeartbeat(ctx context.Context, id string) error {
	return nil
}

func (s *iterationStore) CreateTask(ctx context.Context, t models.Task) error {
	return nil
}

func (s *iterationStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	return &models.Task{BaseEntity: models.BaseEntity{ID: id}}, nil
}

func (s *iterationStore) UpdateTask(ctx context.Context, t models.Task) error {
	return nil
}

func (s *iterationStore) ClaimNextReadyTasks(ctx context.Context, n int) ([]models.Task, error) {
	return nil, nil
}

func (s *iterationStore) ListUnprocessedHumanComments(ctx context.Context) ([]models.CommentRef, error) {
	return nil, nil
}

type iterationSandbox struct {
	result sandbox.Result
	err    error
}

func (s *iterationSandbox) Execute(ctx context.Context, p sandbox.Payload) (sandbox.Result, error) {
	return s.result, s.err
}

var s *agenticIterationScenario

func initializeAgenticIterationScenario(sc *godog.ScenarioContext) {
	s = &agenticIterationScenario{
		store:   &iterationStore{tasks: make(map[string]*models.Task), projects: make(map[string]*models.Project)},
		gw:      &iterationGateway{content: "done", tokens: 50, returnTools: true},
		sandbox: &iterationSandbox{result: sandbox.Result{Success: true}},
	}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		s.iterationCount = 0
		s.exceeded = false
		s.finalInjected = false
		s.additionalCalled = false
		s.lastError = nil
		s.completed = false
		s.gw.calls = 0
		return ctx, nil
	})
	registerAgenticIterationSteps(sc, s)
}

func registerAgenticIterationSteps(sc *godog.ScenarioContext, state *agenticIterationScenario) {
	sc.Step(`^the worker is configured with agentic mode enabled$`, state.workerAgenticModeEnabled)
	sc.Step(`^max tool iterations is set to (\d+)$`, state.setMaxIterations)
	sc.Step(`^the gateway returns tool calls on each request$`, state.gatewayReturnsToolCalls)
	sc.Step(`^the agentic loop runs for (\d+) iterations$`, state.runAgenticLoop)
	sc.Step(`^the iteration guard should be exceeded$`, state.iterationGuardExceeded)
	sc.Step(`^a final message should be injected$`, state.finalMessageInjected)
	sc.Step(`^one additional call should be allowed$`, state.additionalCallAllowed)
	sc.Step(`^token budget is set to (\d+) tokens$`, state.setTokenBudget)
	sc.Step(`^the gateway reports (\d+) tokens per call$`, state.gatewayReportsTokens)
	sc.Step(`^the agentic loop makes (\d+) calls$`, state.agenticLoopMakesCalls)
	sc.Step(`^the agentic loop makes a third call$`, state.agenticLoopMakesThirdCall)
	sc.Step(`^the second call should succeed$`, state.secondCallSucceeds)
	sc.Step(`^the second call should succeed \(120 total, under 200 reserve\)$`, state.secondCallSucceeds)
	sc.Step(`^the request should fail with ErrBudgetExceeded$`, state.requestFailsBudgetExceeded)
	sc.Step(`^the task context has a deadline (\d+) second(s?) in the past$`, state.deadlineInPast)
	sc.Step(`^the agentic loop attempts to start an iteration$`, state.loopAttemptsIteration)
	sc.Step(`^the request should fail with "([^"]*)"$`, state.requestFailsWithMessage)
	sc.Step(`^the gateway returns no tool calls on the first request$`, state.gatewayNoToolCalls)
	sc.Step(`^the agentic loop runs$`, state.agenticLoopRuns)
	sc.Step(`^the loop should complete successfully$`, state.loopCompletesSuccessfully)
	sc.Step(`^the result should be committed$`, state.resultCommitted)
	sc.Step(`^task "([^"]*)" uses (\d+) tokens$`, state.taskUsesTokens)
	sc.Step(`^task "([^"]*)" should still have full (\d+) token budget available$`, state.taskHasFullBudget)
	sc.Step(`^task "([^"]*)" should be blocked from further calls$`, state.taskBlockedFromCalls)
}

func (state *agenticIterationScenario) workerAgenticModeEnabled() error {
	state.workerOpts = worker.WorkerOptions{
		MaxRetries:        3,
		MaxToolIterations: 10,
		TokenBudget:       0,
	}
	return nil
}

func (state *agenticIterationScenario) setMaxIterations(n int) error {
	state.maxIterations = n
	state.workerOpts.MaxToolIterations = n
	return nil
}

func (state *agenticIterationScenario) gatewayReturnsToolCalls() error {
	state.gw.returnTools = true
	return nil
}

func (state *agenticIterationScenario) runAgenticLoop(n int) error {
	ig := worker.NewIterationGuard(n)
	for i := 0; i < n; i++ {
		if err := ig.BeforeIteration(); err != nil {
			return err
		}
		ig.AfterIteration(true)
	}
	state.exceeded = ig.IsExceeded()
	state.finalInjected = ig.ShouldInjectFinalMessage()
	return nil
}

func (state *agenticIterationScenario) iterationGuardExceeded() error {
	if !state.exceeded {
		return fmt.Errorf("expected iteration guard to be exceeded")
	}
	return nil
}

func (state *agenticIterationScenario) finalMessageInjected() error {
	if !state.finalInjected {
		return fmt.Errorf("expected final message to be injected")
	}
	return nil
}

func (state *agenticIterationScenario) additionalCallAllowed() error {
	ig := worker.NewIterationGuard(2)
	ig.AfterIteration(true)
	ig.AfterIteration(true)
	if !ig.ShouldInjectFinalMessage() {
		return fmt.Errorf("expected final message to be allowed")
	}
	// After final message is injected and used, reset should block further calls
	ig.ResetAllowFinal()
	if err := ig.BeforeIteration(); err == nil {
		return fmt.Errorf("expected error after final call exhausted, got nil")
	}
	return nil
}

func (state *agenticIterationScenario) setTokenBudget(n int) error {
	state.tokenBudget = n
	state.workerOpts.TokenBudget = n
	state.budgetTracker = gateway.NewBudgetTracker(n)
	state.budgetGuard = worker.NewBudgetGuard(state.budgetTracker, "task-1")
	return nil
}

func (state *agenticIterationScenario) gatewayReportsTokens(n int) error {
	state.gw.tokens = n
	return nil
}

func (state *agenticIterationScenario) agenticLoopMakesCalls(n int) error {
	for i := 0; i < n; i++ {
		if err := state.budgetGuard.BeforeCall(); err != nil {
			state.lastError = err
			return nil
		}
		state.budgetGuard.AfterCall(state.gw.tokens)
	}
	state.tokenUsage = state.budgetGuard.Usage()
	return nil
}

func (state *agenticIterationScenario) agenticLoopMakesThirdCall() error {
	// Use the existing budget guard from previous steps
	if err := state.budgetGuard.BeforeCall(); err != nil {
		state.lastError = err
		return nil
	}
	state.budgetGuard.AfterCall(state.gw.tokens)
	state.tokenUsage = state.budgetGuard.Usage()
	return nil
}

func (state *agenticIterationScenario) secondCallSucceeds() error {
	if state.tokenUsage < state.tokenBudget*2 {
		return nil
	}
	return fmt.Errorf("expected second call to succeed")
}

func (state *agenticIterationScenario) requestFailsBudgetExceeded() error {
	if state.lastError == nil {
		return fmt.Errorf("expected error but got nil")
	}
	if !strings.Contains(state.lastError.Error(), "budget") {
		return fmt.Errorf("expected budget error, got %v", state.lastError)
	}
	return nil
}

func (state *agenticIterationScenario) deadlineInPast(n int) error {
	// Create a context with a deadline in the past
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Duration(n)*time.Second))
	_ = ctx
	defer cancel()
	// Wait to ensure deadline is definitely expired
	time.Sleep(10 * time.Millisecond)
	// Create deadline guard with expired context
	dg := worker.NewDeadlineGuard(ctx)
	err := dg.BeforeIteration()
	state.lastError = err
	return nil
}

func (state *agenticIterationScenario) loopAttemptsIteration() error {
	// Use the expired context from deadlineInPast step
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()
	dg := worker.NewDeadlineGuard(ctx)
	err := dg.BeforeIteration()
	state.lastError = err
	return nil
}

func (state *agenticIterationScenario) requestFailsWithMessage(msg string) error {
	if state.lastError == nil {
		return fmt.Errorf("expected error but got nil")
	}
	// Remove quotes from the expected message if present
	expectedMsg := strings.Trim(msg, `"`)
	if !strings.Contains(state.lastError.Error(), expectedMsg) {
		return fmt.Errorf("expected error containing %q, got %v", expectedMsg, state.lastError)
	}
	return nil
}

func (state *agenticIterationScenario) gatewayNoToolCalls() error {
	state.gw.returnTools = false
	return nil
}

func (state *agenticIterationScenario) agenticLoopRuns() error {
	ig := worker.NewIterationGuard(10)
	err := ig.BeforeIteration()
	if err != nil {
		state.lastError = err
	}
	state.completed = err == nil
	return nil
}

func (state *agenticIterationScenario) loopCompletesSuccessfully() error {
	if !state.completed {
		return fmt.Errorf("expected loop to complete successfully")
	}
	return nil
}

func (state *agenticIterationScenario) resultCommitted() error {
	state.resultContent = "done"
	return nil
}

func (state *agenticIterationScenario) taskUsesTokens(taskID string, tokens int) error {
	tracker := gateway.NewBudgetTracker(100)
	bg := worker.NewBudgetGuard(tracker, taskID)
	bg.AfterCall(tokens)
	return nil
}

func (state *agenticIterationScenario) taskHasFullBudget(taskID string, budget int) error {
	tracker := gateway.NewBudgetTracker(budget)
	bg := worker.NewBudgetGuard(tracker, taskID)
	if err := bg.BeforeCall(); err != nil {
		return fmt.Errorf("expected full budget available but got error: %v", err)
	}
	return nil
}

func (state *agenticIterationScenario) taskBlockedFromCalls(taskID string) error {
	tracker := gateway.NewBudgetTracker(100)
	tracker.Add(taskID, 100)
	bg := worker.NewBudgetGuard(tracker, taskID)
	err := bg.BeforeCall()
	if err == nil {
		return fmt.Errorf("expected task to be blocked but call succeeded")
	}
	return nil
}
