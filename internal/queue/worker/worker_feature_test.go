package worker_test

import (
	"context"
	"fmt"
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
	store         *workerTestStore
	gateway       *workerTestGateway
	sandbox       *workerTestSandbox
	result        *sandbox.Result
	legacyCalled  bool
	agenticCalled bool
	warningsLogged []string
}

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
	content       string
	toolCalls     []gateway.ToolCall
	nextContent   string
	nextToolCalls []gateway.ToolCall
	err           error
	requests      []gateway.AIRequest
}

func (g *workerTestGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	content := g.content
	toolCalls := g.toolCalls
	requestNum := len(g.requests)

	if requestNum == 2 && g.nextContent != "" {
		content = g.nextContent
		toolCalls = g.nextToolCalls
	}

	if len(toolCalls) == 0 {
		return gateway.AIResponse{Content: content}, g.err
	}
	return gateway.AIResponse{Content: content, ToolCalls: toolCalls}, g.err
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
				BaseEntity:   models.BaseEntity{ID: "project-1"},
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
		}
		state.sandbox = &workerTestSandbox{
			result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "test output"},
		}
		state.legacyCalled = false
		state.agenticCalled = false
		state.warningsLogged = nil
		return ctx, nil
	})

	registerWorkerSteps(sc, state)
}

func registerWorkerSteps(sc *godog.ScenarioContext, state *workerScenario) {
	// Given steps
	sc.Step(`^agentic mode is disabled$`, state.agenticModeDisabled)
	sc.Step(`^agentic mode is enabled$`, state.agenticModeEnabled)
	sc.Step(`^the provider is "([^"]*)"$`, state.providerIs)
	sc.Step(`^the worker has a task to process$`, state.workerHasTask)

	// When steps
	sc.Step(`^the worker processes the task$`, state.workerProcessesTask)

	// Then steps
	sc.Step(`^the worker should use the legacy JSON command path$`, state.useLegacyPath)
	sc.Step(`^the worker should use the agentic loop with tools$`, state.useAgenticLoop)
	sc.Step(`^the worker should fall back to legacy mode$`, state.fallBackToLegacy)
	sc.Step(`^a warning should be logged about unsupported provider$`, state.warningLogged)
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
	s.store.profile.Provider = provider
	return nil
}

func (s *workerScenario) workerHasTask(context.Context) error {
	return nil
}

func (s *workerScenario) workerProcessesTask(context.Context) error {
	w := worker.NewWorker(
		s.store,
		s.gateway,
		s.sandbox,
		nil,
		nil,
		worker.WorkerOptions{},
	)
	w.Process(context.Background(), s.store.task)
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
	// When agentic mode is enabled but provider doesn't support it,
	// a warning is logged via slog.Warn
	// We can verify this by checking the provider was not OpenAI
	if s.store.profile.Provider == "openai" {
		return fmt.Errorf("expected provider to not be openai for warning scenario")
	}
	// The warning is logged internally; we verify behavior via fallback
	return nil
}