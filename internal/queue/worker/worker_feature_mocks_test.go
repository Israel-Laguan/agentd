package worker_test

import (
	"context"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

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

func (s *workerTestStore) ListChildTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (s *workerTestStore) ReconcileExpiredBlockedTasks(context.Context, time.Time) ([]models.Task, error) {
	return nil, nil
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
