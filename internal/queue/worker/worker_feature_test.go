package worker_test

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/worker"
	"agentd/internal/sandbox"

	"github.com/cucumber/godog"
)

func TestWorkerAgenticModeFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeWorkerScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run worker feature tests")
	}
}

type workerScenario struct {
	store          *workerTestStore
	gateway        *workerTestGateway
	sandbox        *workerTestSandbox
	legacyCalled   bool
	agenticCalled  bool
	warningsLogged []string
	maxIterations  int
	maxRetries     int
	logHandler     *testLogHandler
}

// testLogHandler captures slog records during test execution.
type testLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *testLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *testLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *testLogHandler) WithGroup(_ string) slog.Handler      { return h }

type workerTestStore struct {
	task    models.Task
	project models.Project
	profile models.AgentProfile
	result  *models.TaskResult
}

func (s *workerTestStore) MarkTaskRunning(_ context.Context, id string, _ time.Time, pid int) (*models.Task, error) {
	s.task.State = models.TaskStateRunning
	return &s.task, nil
}

func (s *workerTestStore) UpdateTaskHeartbeat(context.Context, string) error {
	return nil
}

func (s *workerTestStore) IncrementRetryCount(_ context.Context, _ string, _ time.Time) (*models.Task, error) {
	s.task.RetryCount++
	return &s.task, nil
}

func (s *workerTestStore) UpdateTaskState(_ context.Context, _ string, _ time.Time, next models.TaskState) (*models.Task, error) {
	s.task.State = next
	return &s.task, nil
}

func (s *workerTestStore) UpdateTaskResult(_ context.Context, _ string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	s.result = &result
	if result.Success {
		s.task.State = models.TaskStateCompleted
	} else {
		s.task.State = models.TaskStateFailed
	}
	return &s.task, nil
}

func (s *workerTestStore) AddComment(context.Context, models.Comment) error {
	return nil
}

func (s *workerTestStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return nil, nil
}

func (s *workerTestStore) ListCommentsSince(context.Context, string, time.Time) ([]models.Comment, error) {
	return nil, nil
}

func (s *workerTestStore) GetProject(context.Context, string) (*models.Project, error) {
	return &s.project, nil
}

func (s *workerTestStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return &s.profile, nil
}

func (s *workerTestStore) GetTask(context.Context, string) (*models.Task, error) {
	return &s.task, nil
}

func (s *workerTestStore) Close() error {
	return nil
}

func (s *workerTestStore) AppendEvent(context.Context, models.Event) error {
	return nil
}

func (s *workerTestStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}

func (s *workerTestStore) MarkEventsCurated(context.Context, string) error {
	return nil
}

func (s *workerTestStore) DeleteCuratedEvents(context.Context, string) error {
	return nil
}

func (s *workerTestStore) ListCompletedTasksOlderThan(_ context.Context, _ time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (s *workerTestStore) RecordMemory(context.Context, models.Memory) error {
	return nil
}

func (s *workerTestStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}

func (s *workerTestStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}

func (s *workerTestStore) TouchMemories(context.Context, []string) error {
	return nil
}

func (s *workerTestStore) SupersedeMemories(context.Context, []string, string) error {
	return nil
}

func (s *workerTestStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}

func (s *workerTestStore) UpsertAgentProfile(context.Context, models.AgentProfile) error {
	return nil
}

func (s *workerTestStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return []models.AgentProfile{s.profile}, nil
}

func (s *workerTestStore) DeleteAgentProfile(context.Context, string) error {
	return nil
}

func (s *workerTestStore) AssignTaskAgent(_ context.Context, _ string, _ time.Time, _ string) (*models.Task, error) {
	return &s.task, nil
}

func (s *workerTestStore) ListSettings(context.Context) ([]models.Setting, error) {
	return nil, nil
}

func (s *workerTestStore) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (s *workerTestStore) SetSetting(context.Context, string, string) error {
	return nil
}

func (s *workerTestStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}

func (s *workerTestStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{}, nil
}

func (s *workerTestStore) EnsureProjectTask(context.Context, string, models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{}, true, nil
}

func (s *workerTestStore) ListProjects(context.Context) ([]models.Project, error) {
	return nil, nil
}

func (s *workerTestStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (s *workerTestStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) {
	return nil, nil
}

func (s *workerTestStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}

func (s *workerTestStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (s *workerTestStore) ReconcileStaleTasks(_ context.Context, _ []int, _ time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (s *workerTestStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}

func (s *workerTestStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, _ []models.DraftTask) (*models.Task, []models.Task, error) {
	return &models.Task{}, nil, nil
}

func (s *workerTestStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}

func (s *workerTestStore) MarkCommentProcessed(context.Context, string, string) error {
	return nil
}

type workerTestGateway struct {
	content               string
	toolCalls             []gateway.ToolCall
	nextContent           string
	err                   error
	requests              []gateway.AIRequest
	callCount             int
	returnToolCalls       bool // For simulating sequence: first returns tool calls, then plain text
	returnsPlainText      bool // Flag to indicate next call should return plain text
	lastResponseToolCalls int  // Track tool calls count in the last response
}

func (g *workerTestGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	g.callCount++

	// Handle sequence: first call returns tool calls, second returns plain text
	if g.returnToolCalls && g.callCount == 1 {
		resp := gateway.AIResponse{Content: "I'll execute a command", ToolCalls: g.toolCalls}
		g.lastResponseToolCalls = len(g.toolCalls)
		return resp, g.err
	}

	if g.returnsPlainText && g.callCount == 2 {
		resp := gateway.AIResponse{Content: g.nextContent, ToolCalls: nil}
		g.lastResponseToolCalls = 0
		return resp, g.err
	}

	// For max iterations test - always return tool calls
	if g.returnToolCalls && len(g.toolCalls) > 0 {
		resp := gateway.AIResponse{Content: "Executing command", ToolCalls: g.toolCalls}
		g.lastResponseToolCalls = len(g.toolCalls)
		return resp, g.err
	}

	if len(g.toolCalls) == 0 {
		resp := gateway.AIResponse{Content: g.content}
		g.lastResponseToolCalls = 0
		return resp, g.err
	}
	resp := gateway.AIResponse{Content: g.content, ToolCalls: g.toolCalls}
	g.lastResponseToolCalls = len(g.toolCalls)
	return resp, g.err
}

func (g *workerTestGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *workerTestGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *workerTestGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

type workerTestSandbox struct {
	result   sandbox.Result
	commands []string
}

func (s *workerTestSandbox) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	s.commands = append(s.commands, payload.Command)
	return s.result, nil
}

func initializeWorkerScenario(sc *godog.ScenarioContext) {
	state := &workerScenario{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		state.store = &workerTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-1"},
				ProjectID:  "project-1",
				AgentID:    "default",
				State:      models.TaskStateQueued,
			},
			project: models.Project{
				BaseEntity:    models.BaseEntity{ID: "project-1"},
				WorkspacePath: "/tmp/test-workspace",
			},
			profile: models.AgentProfile{
				ID:       "default",
				Provider: "openai",
				Model:    "gpt-4",
			},
		}
		state.gateway = &workerTestGateway{
			content: `{"command":"echo test"}`,
			toolCalls: []gateway.ToolCall{
				{ID: "call_1", Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo test"}`}},
			},
		}
		state.sandbox = &workerTestSandbox{
			result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "test output"},
		}
		state.legacyCalled = false
		state.agenticCalled = false
		state.warningsLogged = nil
		state.maxIterations = 10
		state.maxRetries = 1
		return ctx, nil
	})

	registerWorkerSteps(sc, state)
}

func registerWorkerSteps(sc *godog.ScenarioContext, state *workerScenario) {
	// Given steps for agentic mode toggle
	sc.Step(`^agentic mode is disabled$`, state.agenticModeDisabled)
	sc.Step(`^agentic mode is enabled$`, state.agenticModeEnabled)
	sc.Step(`^the provider is "([^"]*)"$`, state.providerIs)
	sc.Step(`^the worker has a task to process$`, state.workerHasTask)

	// When steps
	sc.Step(`^the worker processes the task$`, state.workerProcessesTask)
	sc.Step(`^the worker processes a task$`, state.workerProcessesTask)

	// Then steps for agentic mode toggle
	sc.Step(`^the worker should use the legacy JSON command path$`, state.useLegacyPath)
	sc.Step(`^the worker should use the agentic loop with tools$`, state.useAgenticLoop)
	sc.Step(`^the worker should fall back to legacy mode$`, state.fallBackToLegacy)
	sc.Step(`^a warning should be logged about unsupported provider$`, state.warningLogged)

	// Given steps for agentic mode loop
	sc.Step(`^the worker is configured with agentic mode enabled$`, state.agenticModeEnabled)
	sc.Step(`^the gateway will return tool calls on first call$`, state.gatewayReturnsToolCallsFirst)
	sc.Step(`^the gateway will return plain text on second call$`, state.gatewayReturnsPlainTextSecond)
	sc.Step(`^the maximum tool iterations is set to 3$`, state.maxIterationsSetTo3)
	sc.Step(`^the maximum tool retries is set to 1$`, state.maxRetriesSetTo1)
	sc.Step(`^the gateway always returns tool calls$`, state.gatewayAlwaysReturnsToolCalls)

	// Then steps for agentic mode loop
	sc.Step(`^the worker shall call the gateway with tool definitions$`, state.verifyGatewayCalledWithTools)
	sc.Step(`^the gateway returns a response with tool calls$`, state.verifyGatewayReturnedToolCalls)
	sc.Step(`^the worker executes the tool calls$`, state.verifyToolCallsExecuted)
	sc.Step(`^the worker shall append tool result messages to the conversation$`, state.verifyToolResultMessagesAppended)
	sc.Step(`^the gateway returns a response without tool calls$`, state.verifyGatewayNoToolCalls)
	sc.Step(`^the worker shall commit the final text as the task result$`, state.verifyFinalTextCommitted)
	sc.Step(`^the worker shall stop after 3 iterations$`, state.verifyStoppedAfter3Iterations)
	sc.Step(`^the worker shall commit a failure result$`, state.verifyFailureResult)
}

func (s *workerScenario) agenticModeDisabled(context.Context) error {
	s.store.profile.AgenticMode = false
	return nil
}

func (s *workerScenario) agenticModeEnabled(context.Context) error {
	s.store.profile.AgenticMode = true
	return nil
}

func (s *workerScenario) providerIs(_ context.Context, provider string) error {
	// Use lowercase for consistent comparison with agenticProviders
	s.store.profile.Provider = strings.ToLower(provider)
	return nil
}

func (s *workerScenario) workerHasTask(context.Context) error {
	return nil
}

func (s *workerScenario) workerProcessesTask(ctx context.Context) error {
	// Capture slog output during processing for log assertions
	handler := &testLogHandler{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	w := worker.NewWorker(
		s.store,
		s.gateway,
		s.sandbox,
		nil,
		nil,
		worker.WorkerOptions{
			MaxToolIterations: s.maxIterations,
			MaxRetries:        s.maxRetries,
		},
	)
	w.Process(ctx, s.store.task)
	s.logHandler = handler
	return nil
}

func (s *workerScenario) useLegacyPath(context.Context) error {
	// Legacy path uses GenerateJSON which generates 1 request
	// and executes a single sandbox command
	if len(s.gateway.requests) != 1 {
		return fmt.Errorf("expected 1 gateway request for legacy path, got %d", len(s.gateway.requests))
	}
	// Legacy path should NOT include tools in the first request
	if len(s.gateway.requests[0].Tools) > 0 {
		return fmt.Errorf("legacy path should not include tools")
	}
	// Legacy path should execute one sandbox command
	if len(s.sandbox.commands) != 1 {
		return fmt.Errorf("expected 1 sandbox command for legacy path, got %d", len(s.sandbox.commands))
	}
	return nil
}

func (s *workerScenario) useAgenticLoop(context.Context) error {
	// Agentic path makes multiple requests with tools
	// First request should have tools
	if len(s.gateway.requests) == 0 {
		return fmt.Errorf("expected at least 1 gateway request for agentic path, got 0")
	}
	if len(s.gateway.requests[0].Tools) == 0 {
		return fmt.Errorf("agentic path should include tools in first request")
	}
	return nil
}

func (s *workerScenario) fallBackToLegacy(context.Context) error {
	// When falling back to legacy mode, should behave like legacy:
	// - 1 gateway request
	// - No tools in request
	// - 1 sandbox command executed
	if len(s.gateway.requests) != 1 {
		return fmt.Errorf("expected 1 gateway request for fallback path, got %d", len(s.gateway.requests))
	}
	if len(s.gateway.requests[0].Tools) > 0 {
		return fmt.Errorf("fallback path should not include tools")
	}
	if len(s.sandbox.commands) != 1 {
		return fmt.Errorf("expected 1 sandbox command for fallback path, got %d", len(s.sandbox.commands))
	}
	return nil
}

func (s *workerScenario) warningLogged(context.Context) error {
	if s.logHandler == nil {
		return fmt.Errorf("no log handler captured; workerProcessesTask must run first")
	}
	s.logHandler.mu.Lock()
	defer s.logHandler.mu.Unlock()
	for _, r := range s.logHandler.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "agentic mode requested but provider does not support") {
			return nil
		}
	}
	return fmt.Errorf("expected a slog.Warn about unsupported provider, but none was recorded")
}

// New step implementations for agentic mode loop feature

func (s *workerScenario) gatewayReturnsToolCallsFirst(context.Context) error {
	s.gateway.returnToolCalls = true
	s.gateway.toolCalls = []gateway.ToolCall{
		{ID: "call_abc", Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"pwd"}`}},
	}
	return nil
}

func (s *workerScenario) gatewayReturnsPlainTextSecond(context.Context) error {
	s.gateway.returnsPlainText = true
	s.gateway.nextContent = "Task completed successfully. The current directory is /home/user."
	return nil
}

func (s *workerScenario) maxIterationsSetTo3(context.Context) error {
	s.maxIterations = 3
	return nil
}

func (s *workerScenario) maxRetriesSetTo1(context.Context) error {
	s.maxRetries = 1
	return nil
}

func (s *workerScenario) gatewayAlwaysReturnsToolCalls(context.Context) error {
	s.gateway.returnToolCalls = true
	s.gateway.toolCalls = []gateway.ToolCall{
		{ID: "call_1", Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo iteration"}`}},
	}
	return nil
}

func (s *workerScenario) verifyGatewayCalledWithTools(context.Context) error {
	if len(s.gateway.requests) == 0 {
		return fmt.Errorf("expected at least 1 gateway request, got 0")
	}
	if len(s.gateway.requests[0].Tools) == 0 {
		return fmt.Errorf("expected gateway request to include tool definitions")
	}
	return nil
}

func (s *workerScenario) verifyGatewayReturnedToolCalls(context.Context) error {
	// This is verified by the mock returning tool calls on first call
	if s.gateway.callCount < 1 {
		return fmt.Errorf("expected gateway to be called at least once")
	}
	return nil
}

func (s *workerScenario) verifyToolCallsExecuted(context.Context) error {
	// Verify sandbox was called to execute the tool
	if len(s.sandbox.commands) == 0 {
		return fmt.Errorf("expected at least one sandbox command execution")
	}
	return nil
}

func (s *workerScenario) verifyToolResultMessagesAppended(context.Context) error {
	// Tool result messages are appended internally
	// We verify this by checking that gateway was called multiple times
	if s.gateway.callCount < 2 {
		return fmt.Errorf("expected at least 2 gateway calls (tool call + final), got %d", s.gateway.callCount)
	}

	// Also verify that the second request actually contains a tool-result message
	if len(s.gateway.requests) < 2 {
		return fmt.Errorf("expected at least 2 gateway requests recorded, got %d", len(s.gateway.requests))
	}

	// Check the second request for a tool message
	lastReq := s.gateway.requests[len(s.gateway.requests)-1]
	for _, msg := range lastReq.Messages {
		if msg.Role == "tool" {
			// Verify the tool message contains the sandbox output
			if s.sandbox.result.Stdout != "" && !strings.Contains(msg.Content, s.sandbox.result.Stdout) {
				return fmt.Errorf("expected tool message to contain sandbox output %q, got %q", s.sandbox.result.Stdout, msg.Content)
			}
			return nil
		}
	}

	return fmt.Errorf("expected second gateway request to include a tool-result message")
}

func (s *workerScenario) verifyGatewayNoToolCalls(context.Context) error {
	// Verify that the gateway's final response had no tool calls
	if s.gateway.lastResponseToolCalls > 0 {
		return fmt.Errorf("expected no tool calls in gateway response, got %d", s.gateway.lastResponseToolCalls)
	}
	return nil
}

func (s *workerScenario) verifyFinalTextCommitted(context.Context) error {
	// Verify that the final text was committed as the result
	if s.store.result == nil {
		return fmt.Errorf("expected a task result to be committed")
	}
	if !s.store.result.Success {
		return fmt.Errorf("expected successful result")
	}
	// Successful worker results include execution metadata; verify the final text is preserved.
	if s.gateway.nextContent != "" && !strings.Contains(s.store.result.Payload, s.gateway.nextContent) {
		return fmt.Errorf("expected committed payload to contain final text %q, got %q", s.gateway.nextContent, s.store.result.Payload)
	}
	return nil
}

func (s *workerScenario) verifyStoppedAfter3Iterations(context.Context) error {
	// The worker should stop after maxIterations gateway calls.
	if s.gateway.callCount != s.maxIterations {
		return fmt.Errorf("expected %d gateway calls (iteration cap), got %d", s.maxIterations, s.gateway.callCount)
	}
	return nil
}

func (s *workerScenario) verifyFailureResult(context.Context) error {
	if s.store.result == nil {
		return fmt.Errorf("expected a task result to be committed")
	}
	if s.store.result.Success {
		return fmt.Errorf("expected failure result, got success")
	}
	return nil
}
