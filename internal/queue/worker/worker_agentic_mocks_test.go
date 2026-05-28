package worker

import (
	"context"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type fakeCapabilityAdapter struct {
	tools []gateway.ToolDefinition
}

func (f fakeCapabilityAdapter) Name() string { return "fake" }

func (f fakeCapabilityAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityAdapter) CallTool(context.Context, string, map[string]any) (any, error) {
	return nil, nil
}

func (f fakeCapabilityAdapter) Close() error { return nil }

type fakeCapabilityCallAdapter struct {
	name  string
	tools []gateway.ToolDefinition
}

func (f fakeCapabilityCallAdapter) Name() string { return f.name }

func (f fakeCapabilityCallAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityCallAdapter) CallTool(_ context.Context, name string, args map[string]any) (any, error) {
	return map[string]any{"tool": name, "args": args, "adapter": f.name}, nil
}

func (f fakeCapabilityCallAdapter) Close() error { return nil }

type fakeCapabilityErrorAdapter struct {
	name  string
	tools []gateway.ToolDefinition
	err   error
}

func (f fakeCapabilityErrorAdapter) Name() string { return f.name }

func (f fakeCapabilityErrorAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityErrorAdapter) CallTool(context.Context, string, map[string]any) (any, error) {
	return nil, f.err
}

func (f fakeCapabilityErrorAdapter) Close() error { return nil }

func containsTool(tools []gateway.ToolDefinition, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// mockExecSandbox implements sandbox.Executor for testing
type mockExecSandbox struct {
	result sandbox.Result
	err    error
}

func (m *mockExecSandbox) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	return m.result, m.err
}

// mockCommitStore implements models.KanbanStore to support commitText testing
type mockCommitStore struct {
	text          *string
	task          *models.Task
	comments      []models.Comment
	listSinceArgs []time.Time
	blockErr      error
}

func (m *mockCommitStore) MarkTaskRunning(ctx context.Context, id string, t time.Time, pid int) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) UpdateTaskHeartbeat(ctx context.Context, id string) error {
	return nil
}
func (m *mockCommitStore) UpdateCriteriaMet(context.Context, string, []string) error { return nil }

func (m *mockCommitStore) IncrementRetryCount(ctx context.Context, id string, t time.Time) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) UpdateTaskDescription(ctx context.Context, id string, _ time.Time, description string) (*models.Task, error) {
	task, err := m.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, models.ErrTaskNotFound
	}
	updated := *task
	updated.Description = description
	return &updated, nil
}

func (m *mockCommitStore) UpdateTaskState(ctx context.Context, id string, t time.Time, state models.TaskState) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) UpdateTaskPatch(context.Context, string, time.Time, *models.TaskState, *string) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) UpdateTaskResult(ctx context.Context, id string, t time.Time, result models.TaskResult) (*models.Task, error) {
	if m.text != nil && result.Payload != "" {
		*m.text = result.Payload
	}
	return nil, nil
}

func (m *mockCommitStore) AddComment(ctx context.Context, c models.Comment) error {
	return nil
}

func (m *mockCommitStore) ListComments(ctx context.Context, id string) ([]models.Comment, error) {
	return append([]models.Comment(nil), m.comments...), nil
}

func (m *mockCommitStore) ListCommentsSince(ctx context.Context, id string, since time.Time) ([]models.Comment, error) {
	m.listSinceArgs = append(m.listSinceArgs, since)
	var out []models.Comment
	for _, c := range m.comments {
		if since.IsZero() || c.UpdatedAt.After(since) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *mockCommitStore) GetProject(ctx context.Context, id string) (*models.Project, error) {
	return nil, nil
}

func (m *mockCommitStore) GetAgentProfile(ctx context.Context, id string) (*models.AgentProfile, error) {
	return nil, nil
}

func (m *mockCommitStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	if m.task != nil && m.task.ID == id {
		t := *m.task
		return &t, nil
	}
	return nil, models.ErrTaskNotFound
}

func (m *mockCommitStore) Close() error {
	return nil
}

func (m *mockCommitStore) AppendEvent(ctx context.Context, e models.Event) error {
	return nil
}

func (m *mockCommitStore) ListEventsByTask(ctx context.Context, id string) ([]models.Event, error) {
	return nil, nil
}

func (m *mockCommitStore) MarkEventsCurated(ctx context.Context, id string) error {
	return nil
}

func (m *mockCommitStore) DeleteCuratedEvents(ctx context.Context, id string) error {
	return nil
}

func (m *mockCommitStore) ListCompletedTasksOlderThan(ctx context.Context, d time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) RecordMemory(ctx context.Context, mem models.Memory) error {
	return nil
}

func (m *mockCommitStore) ListMemories(ctx context.Context, f models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockCommitStore) RecallMemories(ctx context.Context, q models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockCommitStore) TouchMemories(ctx context.Context, ids []string) error {
	return nil
}

func (m *mockCommitStore) SupersedeMemories(ctx context.Context, oldIDs []string, newID string) error {
	return nil
}

func (m *mockCommitStore) ListUnsupersededMemories(ctx context.Context) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockCommitStore) UpsertAgentProfile(ctx context.Context, p models.AgentProfile) error {
	return nil
}

func (m *mockCommitStore) ListAgentProfiles(ctx context.Context) ([]models.AgentProfile, error) {
	return nil, nil
}

func (m *mockCommitStore) DeleteAgentProfile(ctx context.Context, id string) error {
	return nil
}

func (m *mockCommitStore) AssignTaskAgent(ctx context.Context, taskID string, t time.Time, agentID string) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ListSettings(ctx context.Context) ([]models.Setting, error) {
	return nil, nil
}

func (m *mockCommitStore) GetSetting(ctx context.Context, key string) (string, bool, error) {
	return "", false, nil
}

func (m *mockCommitStore) SetSetting(ctx context.Context, key, value string) error {
	return nil
}

func (m *mockCommitStore) MaterializePlan(ctx context.Context, dp models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}

func (m *mockCommitStore) MarkProjectTasksReady(ctx context.Context, projectID string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) EnsureSystemProject(ctx context.Context) (*models.Project, error) {
	return nil, nil
}

func (m *mockCommitStore) EnsureProjectTask(ctx context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	return nil, false, nil
}

func (m *mockCommitStore) ListProjects(ctx context.Context) ([]models.Project, error) {
	return nil, nil
}

func (m *mockCommitStore) ListTasksByProject(ctx context.Context, projectID string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ClaimNextReadyTasks(ctx context.Context, limit int) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ReconcileGhostTasks(ctx context.Context, alivePIDs []int) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ReconcileStaleTasks(ctx context.Context, alivePIDs []int, staleThreshold time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) AppendTasksToProject(ctx context.Context, projectID, parentTaskID string, drafts []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) BlockTaskWithSubtasks(ctx context.Context, taskID string, t time.Time, subtasks []models.DraftTask) (*models.Task, []models.Task, error) {
	if m.blockErr != nil {
		return nil, nil, m.blockErr
	}
	return nil, nil, nil
}

func (m *mockCommitStore) ListParentTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ListChildTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ReconcileExpiredBlockedTasks(context.Context, time.Time) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ListUnprocessedHumanComments(ctx context.Context) ([]models.CommentRef, error) {
	return nil, nil
}

func (m *mockCommitStore) MarkCommentProcessed(ctx context.Context, taskID, commentEventID string) error {
	return nil
}
