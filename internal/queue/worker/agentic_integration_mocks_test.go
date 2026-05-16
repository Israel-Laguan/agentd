package worker

import (
	"context"
	"strings"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// mockAgenticSandbox executes commands and returns predefined results
type mockAgenticSandbox struct {
	results        map[string]sandbox.Result
	executionCount int
	lastCommand    string
}

func (m *mockAgenticSandbox) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	m.executionCount++
	m.lastCommand = payload.Command

	// Look up the command in results
	if result, ok := m.results[payload.Command]; ok {
		return result, nil
	}

	// Default result if command not found
	return sandbox.Result{
		Success:  true,
		ExitCode: 0,
		Stdout:   "mock output",
		Stderr:   "",
	}, nil
}

// mockAgenticStore implements the store interface for agentic integration tests
type mockAgenticStore struct {
	task            models.Task
	project         models.Project
	profile         models.AgentProfile
	committedResult *models.TaskResult
}

func newMockAgenticStore(taskID string) *mockAgenticStore {
	store := &mockAgenticStore{
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: taskID},
			ProjectID:  "project-1",
			AgentID:    "agent-1",
			Title:      "Run agentic loop",
			State:      models.TaskStateQueued,
		},
		project: models.Project{
			BaseEntity:    models.BaseEntity{ID: "project-1"},
			WorkspacePath: "/tmp/test-workspace",
		},
		profile: models.AgentProfile{
			ID:          "agent-1",
			Provider:    "openai",
			Model:       "gpt-4",
			AgenticMode: true,
		},
	}
	return store
}

type recordingCapabilityAdapter struct {
	tools     []gateway.ToolDefinition
	result    any
	err       error
	callCount int
	lastName  string
	lastArgs  map[string]any
}

func (r *recordingCapabilityAdapter) Name() string { return "recording" }

func (r *recordingCapabilityAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return r.tools, nil
}

func (r *recordingCapabilityAdapter) CallTool(_ context.Context, name string, args map[string]any) (any, error) {
	r.callCount++
	r.lastName = name
	r.lastArgs = args
	if r.err != nil {
		return nil, r.err
	}
	if r.result == nil {
		return map[string]any{"ok": true}, nil
	}
	return r.result, nil
}

func (r *recordingCapabilityAdapter) Close() error { return nil }

func requestContainsTool(req gateway.AIRequest, name string) bool {
	for _, tool := range req.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func requestContainsToolResult(req gateway.AIRequest, toolCallID, contentSubstring string) bool {
	for _, message := range req.Messages {
		if message.Role == "tool" && message.ToolCallID == toolCallID && strings.Contains(message.Content, contentSubstring) {
			return true
		}
	}
	return false
}

func (m *mockAgenticStore) MarkTaskRunning(_ context.Context, id string, _ time.Time, pid int) (*models.Task, error) {
	m.task.ID = id
	m.task.State = models.TaskStateRunning
	return &m.task, nil
}

func (m *mockAgenticStore) UpdateTaskHeartbeat(context.Context, string) error {
	return nil
}

func (m *mockAgenticStore) IncrementRetryCount(_ context.Context, _ string, _ time.Time) (*models.Task, error) {
	m.task.RetryCount++
	return &m.task, nil
}

func (m *mockAgenticStore) UpdateTaskState(_ context.Context, _ string, _ time.Time, next models.TaskState) (*models.Task, error) {
	m.task.State = next
	return &m.task, nil
}

func (m *mockAgenticStore) UpdateTaskResult(_ context.Context, _ string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	m.committedResult = &result
	if result.Success {
		m.task.State = models.TaskStateCompleted
	} else {
		m.task.State = models.TaskStateFailed
	}
	return &m.task, nil
}

func (m *mockAgenticStore) AddComment(context.Context, models.Comment) error {
	return nil
}

func (m *mockAgenticStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return nil, nil
}

func (m *mockAgenticStore) ListCommentsSince(context.Context, string, time.Time) ([]models.Comment, error) {
	return nil, nil
}

func (m *mockAgenticStore) GetProject(context.Context, string) (*models.Project, error) {
	return &m.project, nil
}

func (m *mockAgenticStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return &m.profile, nil
}

func (m *mockAgenticStore) GetTask(context.Context, string) (*models.Task, error) {
	return &m.task, nil
}

func (m *mockAgenticStore) Close() error {
	return nil
}

func (m *mockAgenticStore) AppendEvent(context.Context, models.Event) error {
	return nil
}

func (m *mockAgenticStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}

func (m *mockAgenticStore) MarkEventsCurated(context.Context, string) error {
	return nil
}

func (m *mockAgenticStore) DeleteCuratedEvents(context.Context, string) error {
	return nil
}

func (m *mockAgenticStore) ListCompletedTasksOlderThan(_ context.Context, _ time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) RecordMemory(context.Context, models.Memory) error {
	return nil
}

func (m *mockAgenticStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockAgenticStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockAgenticStore) TouchMemories(context.Context, []string) error {
	return nil
}

func (m *mockAgenticStore) SupersedeMemories(context.Context, []string, string) error {
	return nil
}

func (m *mockAgenticStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockAgenticStore) UpsertAgentProfile(context.Context, models.AgentProfile) error {
	return nil
}

func (m *mockAgenticStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return []models.AgentProfile{m.profile}, nil
}

func (m *mockAgenticStore) DeleteAgentProfile(context.Context, string) error {
	return nil
}

func (m *mockAgenticStore) AssignTaskAgent(_ context.Context, _ string, _ time.Time, _ string) (*models.Task, error) {
	return &m.task, nil
}

func (m *mockAgenticStore) ListSettings(context.Context) ([]models.Setting, error) {
	return nil, nil
}

func (m *mockAgenticStore) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (m *mockAgenticStore) SetSetting(context.Context, string, string) error {
	return nil
}

func (m *mockAgenticStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}

func (m *mockAgenticStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{}, nil
}

func (m *mockAgenticStore) EnsureProjectTask(context.Context, string, models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{}, true, nil
}

func (m *mockAgenticStore) ListProjects(context.Context) ([]models.Project, error) {
	return nil, nil
}

func (m *mockAgenticStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ReconcileStaleTasks(_ context.Context, _ []int, _ time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, _ []models.DraftTask) (*models.Task, []models.Task, error) {
	return &m.task, nil, nil
}

func (m *mockAgenticStore) ListChildTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ReconcileExpiredBlockedTasks(context.Context, time.Time) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}

func (m *mockAgenticStore) MarkCommentProcessed(context.Context, string, string) error {
	return nil
}
