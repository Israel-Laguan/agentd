package worker

import (
	"context"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// TestProviderSupportsAgentic_ReturnsTrueForOpenAI verifies that providerSupportsAgentic
// returns true for OpenAI provider.
// Validates: Requirements 1, 3, 4, 6.2
func TestProviderSupportsAgentic_ReturnsTrueForOpenAI(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
		expected bool
	}{
		{"OpenAI lowercase", "openai", true},
		{"OpenAI uppercase", "OPENAI", true},
		{"OpenAI mixed case", "OpenAI", true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &Worker{}
			profile := models.AgentProfile{
				ID:       "test",
				Provider: tc.provider,
				Model:    "gpt-4",
			}

			result := w.providerSupportsAgentic(profile)
			if result != tc.expected {
				t.Errorf("providerSupportsAgentic(%q) = %v, want %v", tc.provider, result, tc.expected)
			}
		})
	}
}

// TestProviderSupportsAgentic_ReturnsFalseForOtherProviders verifies that providerSupportsAgentic
// returns false for providers other than OpenAI.
// Validates: Requirements 1, 3, 4, 6.2
func TestProviderSupportsAgentic_ReturnsFalseForOtherProviders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"Anthropic", "anthropic"},
		{"Anthropic uppercase", "ANTHROPIC"},
		{"Anthropic mixed case", "Anthropic"},
		{"Ollama", "ollama"},
		{"Ollama uppercase", "OLLAMA"},
		{"Azure OpenAI", "azure-openai"},
		{"Vertex", "vertex"},
		{"Empty string", ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &Worker{}
			profile := models.AgentProfile{
				ID:       "test",
				Provider: tc.provider,
				Model:    "claude-3",
			}

			result := w.providerSupportsAgentic(profile)
			if result {
				t.Errorf("providerSupportsAgentic(%q) = true, want false", tc.provider)
			}
		})
	}
}

// routingTestStore is a minimal mock store for routing tests that lets us
// inspect which execution path Worker.Process took.
type routingTestStore struct {
	task    models.Task
	project models.Project
	profile models.AgentProfile
	result  *models.TaskResult
}

func (s *routingTestStore) MarkTaskRunning(_ context.Context, _ string, _ time.Time, _ int) (*models.Task, error) {
	s.task.State = models.TaskStateRunning
	return &s.task, nil
}
func (s *routingTestStore) UpdateTaskHeartbeat(context.Context, string) error { return nil }
func (s *routingTestStore) IncrementRetryCount(_ context.Context, _ string, _ time.Time) (*models.Task, error) {
	s.task.RetryCount++
	return &s.task, nil
}
func (s *routingTestStore) UpdateTaskState(_ context.Context, _ string, _ time.Time, next models.TaskState) (*models.Task, error) {
	s.task.State = next
	return &s.task, nil
}
func (s *routingTestStore) UpdateTaskResult(_ context.Context, _ string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	s.result = &result
	if result.Success {
		s.task.State = models.TaskStateCompleted
	} else {
		s.task.State = models.TaskStateFailed
	}
	return &s.task, nil
}
func (s *routingTestStore) AddComment(context.Context, models.Comment) error                 { return nil }
func (s *routingTestStore) ListComments(context.Context, string) ([]models.Comment, error)   { return nil, nil }
func (s *routingTestStore) GetProject(context.Context, string) (*models.Project, error)      { return &s.project, nil }
func (s *routingTestStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return &s.profile, nil
}
func (s *routingTestStore) GetTask(context.Context, string) (*models.Task, error)    { return &s.task, nil }
func (s *routingTestStore) Close() error                                             { return nil }
func (s *routingTestStore) AppendEvent(context.Context, models.Event) error          { return nil }
func (s *routingTestStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}
func (s *routingTestStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (s *routingTestStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (s *routingTestStore) ListCompletedTasksOlderThan(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *routingTestStore) RecordMemory(context.Context, models.Memory) error { return nil }
func (s *routingTestStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}
func (s *routingTestStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}
func (s *routingTestStore) TouchMemories(context.Context, []string) error         { return nil }
func (s *routingTestStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (s *routingTestStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}
func (s *routingTestStore) UpsertAgentProfile(context.Context, models.AgentProfile) error { return nil }
func (s *routingTestStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return []models.AgentProfile{s.profile}, nil
}
func (s *routingTestStore) DeleteAgentProfile(context.Context, string) error { return nil }
func (s *routingTestStore) AssignTaskAgent(_ context.Context, _ string, _ time.Time, _ string) (*models.Task, error) {
	return &s.task, nil
}
func (s *routingTestStore) ListSettings(context.Context) ([]models.Setting, error)    { return nil, nil }
func (s *routingTestStore) GetSetting(context.Context, string) (string, bool, error)  { return "", false, nil }
func (s *routingTestStore) SetSetting(context.Context, string, string) error          { return nil }
func (s *routingTestStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}
func (s *routingTestStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{}, nil
}
func (s *routingTestStore) EnsureProjectTask(context.Context, string, models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{}, true, nil
}
func (s *routingTestStore) ListProjects(context.Context) ([]models.Project, error)         { return nil, nil }
func (s *routingTestStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *routingTestStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error)  { return nil, nil }
func (s *routingTestStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}
func (s *routingTestStore) ReconcileStaleTasks(_ context.Context, _ []int, _ time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (s *routingTestStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}
func (s *routingTestStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, _ []models.DraftTask) (*models.Task, []models.Task, error) {
	return &s.task, nil, nil
}
func (s *routingTestStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}
func (s *routingTestStore) MarkCommentProcessed(context.Context, string, string) error { return nil }

// routingTestGateway records requests so tests can inspect whether the agentic
// or legacy path was taken.
type routingTestGateway struct {
	requests []gateway.AIRequest
}

func (g *routingTestGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	return gateway.AIResponse{Content: `{"command":"echo ok"}`}, nil
}
func (g *routingTestGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error)         { return nil, nil }
func (g *routingTestGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error)    { return nil, nil }
func (g *routingTestGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) { return nil, nil }

// routingTestSandbox records executions.
type routingTestSandbox struct {
	execCount int
}

func (s *routingTestSandbox) Execute(_ context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	s.execCount++
	return sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}, nil
}

// newRoutingTest creates a Worker with mock dependencies for routing tests.
func newRoutingTest(profile models.AgentProfile) (*Worker, *routingTestStore, *routingTestGateway, *routingTestSandbox) {
	store := &routingTestStore{
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: "task-routing"},
			ProjectID:  "project-1",
			AgentID:    "agent-1",
			State:      models.TaskStateQueued,
		},
		project: models.Project{
			BaseEntity:    models.BaseEntity{ID: "project-1"},
			WorkspacePath: "/tmp/test-workspace",
		},
		profile: profile,
	}
	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	return w, store, gw, sb
}

// TestRoutingDecision_AgenticModeFalse_LegacyPath verifies by calling Process
// that when AgenticMode is false, the gateway receives JSONMode requests (legacy path).
// Validates: Requirements 1, 3, 6.2
func TestRoutingDecision_AgenticModeFalse_LegacyPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"OpenAI", "openai"},
		{"Anthropic", "anthropic"},
		{"Ollama", "ollama"},
		{"Empty provider", ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := models.AgentProfile{
				ID:          "agent-1",
				Provider:    tc.provider,
				Model:       "gpt-4",
				AgenticMode: false,
			}
			w, store, gw, _ := newRoutingTest(profile)
			w.Process(context.Background(), store.task)

			// Legacy path: gateway request should have JSONMode=true and no tools
			if len(gw.requests) == 0 {
				t.Fatal("expected at least 1 gateway request")
			}
			if !gw.requests[0].JSONMode {
				t.Error("expected JSONMode=true for legacy path")
			}
			if len(gw.requests[0].Tools) > 0 {
				t.Error("expected no tools in legacy path request")
			}
		})
	}
}

// TestRoutingDecision_AgenticModeTrue_ProviderSupported verifies by calling Process
// that when AgenticMode is true and provider is OpenAI, the agentic path is taken.
// Validates: Requirements 1, 3, 6.2
func TestRoutingDecision_AgenticModeTrue_ProviderSupported(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	w, store, gw, _ := newRoutingTest(profile)
	w.Process(context.Background(), store.task)

	// Agentic path: gateway request should include tools and NOT use JSONMode
	if len(gw.requests) == 0 {
		t.Fatal("expected at least 1 gateway request")
	}
	if gw.requests[0].JSONMode {
		t.Error("expected JSONMode=false for agentic path")
	}
	if len(gw.requests[0].Tools) == 0 {
		t.Error("expected tools in agentic path request")
	}
}

// TestRoutingDecision_AgenticModeTrue_ProviderNotSupported verifies by calling Process
// that when AgenticMode is true but provider doesn't support it, the legacy path is taken.
// Validates: Requirements 1, 3, 4, 6.2
func TestRoutingDecision_AgenticModeTrue_ProviderNotSupported(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"Anthropic", "anthropic"},
		{"Ollama", "ollama"},
		{"Azure OpenAI", "azure-openai"},
		{"Vertex", "vertex"},
		{"Empty provider", ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := models.AgentProfile{
				ID:          "agent-1",
				Provider:    tc.provider,
				Model:       "claude-3",
				AgenticMode: true,
			}
			w, store, gw, _ := newRoutingTest(profile)
			w.Process(context.Background(), store.task)

			// Fallback to legacy: should use JSONMode, no tools
			if len(gw.requests) == 0 {
				t.Fatal("expected at least 1 gateway request")
			}
			if !gw.requests[0].JSONMode {
				t.Errorf("expected JSONMode=true for legacy fallback with provider %q", tc.provider)
			}
			if len(gw.requests[0].Tools) > 0 {
				t.Errorf("expected no tools in legacy fallback for provider %q", tc.provider)
			}
		})
	}
}

// TestAgenticMode_DefaultIsFalse verifies that the default value of AgenticMode is false.
// Validates: Requirement 2.1, 2.2
func TestAgenticMode_DefaultIsFalse(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{}
	if profile.AgenticMode != false {
		t.Errorf("AgenticMode default = %v, want false", profile.AgenticMode)
	}
}

// TestAgenticMode_CanBeSet verifies that AgenticMode can be set to true.
// Validates: Requirement 1.1
func TestAgenticMode_CanBeSet(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{AgenticMode: true}
	if !profile.AgenticMode {
		t.Error("AgenticMode should be settable to true")
	}
}

// TestProviderSupportsAgentic_ImportFromGateway verifies that the provider constant
// is correctly imported from gateway package.
// Validates: Requirement 3.3
func TestProviderSupportsAgentic_ImportFromGateway(t *testing.T) {
	t.Parallel()

	// Verify gateway.ProviderOpenAI is accessible and has correct value
	if string(gateway.ProviderOpenAI) != "openai" {
		t.Errorf("gateway.ProviderOpenAI = %q, want \"openai\"", gateway.ProviderOpenAI)
	}
}