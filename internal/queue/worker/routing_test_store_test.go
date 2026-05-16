package worker

import (
	"context"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

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
func (s *routingTestStore) AddComment(context.Context, models.Comment) error { return nil }
func (s *routingTestStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return nil, nil
}
func (s *routingTestStore) ListCommentsSince(context.Context, string, time.Time) ([]models.Comment, error) {
	return nil, nil
}
func (s *routingTestStore) GetProject(context.Context, string) (*models.Project, error) {
	return &s.project, nil
}
func (s *routingTestStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return &s.profile, nil
}
func (s *routingTestStore) GetTask(context.Context, string) (*models.Task, error) {
	return &s.task, nil
}
func (s *routingTestStore) Close() error                                    { return nil }
func (s *routingTestStore) AppendEvent(context.Context, models.Event) error { return nil }
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
func (s *routingTestStore) TouchMemories(context.Context, []string) error             { return nil }
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
func (s *routingTestStore) ListSettings(context.Context) ([]models.Setting, error) { return nil, nil }
func (s *routingTestStore) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (s *routingTestStore) SetSetting(context.Context, string, string) error { return nil }
func (s *routingTestStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}
func (s *routingTestStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{}, nil
}
func (s *routingTestStore) EnsureProjectTask(context.Context, string, models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{}, true, nil
}
func (s *routingTestStore) ListProjects(context.Context) ([]models.Project, error) { return nil, nil }
func (s *routingTestStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (s *routingTestStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) {
	return nil, nil
}
func (s *routingTestStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}
func (s *routingTestStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
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

func (s *routingTestStore) ListChildTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (s *routingTestStore) ReconcileExpiredBlockedTasks(context.Context, time.Time) ([]models.Task, error) {
	return nil, nil
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
func (g *routingTestGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *routingTestGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *routingTestGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

// routingTestSandbox records executions.
type routingTestSandbox struct {
	execCount int
}

func (s *routingTestSandbox) Execute(_ context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	s.execCount++
	return sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}, nil
}
