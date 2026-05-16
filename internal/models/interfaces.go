package models

import (
	"context"
	"time"
)

// EventSink persists and broadcasts domain events.
type EventSink interface {
	Emit(ctx context.Context, ev Event) error
}

// TaskCanceller cancels active worker contexts for a given task.
type TaskCanceller interface {
	Cancel(taskID string) bool
}

// KanbanBoardContract captures the proposal-aligned box contract for queue and
// frontdesk orchestration. The richer KanbanStore below remains the primary
// internal persistence boundary.
type KanbanBoardContract interface {
	MaterializePlan(ctx context.Context, plan DraftPlan) (*Project, []Task, error)
	ListProjectsPage(ctx context.Context, params PaginationParams) (PaginatedResult[Project], error)
	ListTasks(ctx context.Context, filter TaskFilter) (PaginatedResult[Task], error)
	ClaimNextReadyTasks(ctx context.Context, limit int) ([]Task, error)
	UpdateTaskResult(ctx context.Context, id string, expectedUpdatedAt time.Time, result TaskResult) (*Task, error)
	AddCommentAndPause(ctx context.Context, taskID string, comment Comment) error
	ReconcileGhostTasks(ctx context.Context, alivePIDs []int) ([]Task, error)
}

// AIGatewayContract captures proposal-aligned capabilities used by frontdesk
// and worker flows, independent of provider-specific routing methods.
type AIGatewayContract interface {
	GenerateText(ctx context.Context, prompt string, limit int) (string, error)
	GenerateStructuredJSON(ctx context.Context, prompt string, target interface{}) error
	TruncateToBudget(input string, maxTokens int) string
}

// SandboxEnvironment models the physical execution boundary.
type SandboxEnvironment interface {
	Execute(ctx context.Context, payload ExecutionPayload) ExecutionResult
	CleanupZombie(pid int) error
}

// KanbanStore is the persistence boundary shared by the daemon, queue, and API.
type KanbanStore interface {
	MaterializePlan(ctx context.Context, plan DraftPlan) (*Project, []Task, error)
	EnsureSystemProject(ctx context.Context) (*Project, error)
	EnsureProjectTask(ctx context.Context, projectID string, draft DraftTask) (*Task, bool, error)
	GetProject(ctx context.Context, id string) (*Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	GetTask(ctx context.Context, id string) (*Task, error)
	ListTasksByProject(ctx context.Context, projectID string) ([]Task, error)
	ClaimNextReadyTasks(ctx context.Context, limit int) ([]Task, error)
	MarkTaskRunning(ctx context.Context, id string, expectedUpdatedAt time.Time, pid int) (*Task, error)
	UpdateTaskHeartbeat(ctx context.Context, id string) error
	IncrementRetryCount(ctx context.Context, id string, expectedUpdatedAt time.Time) (*Task, error)
	UpdateTaskState(ctx context.Context, id string, expectedUpdatedAt time.Time, next TaskState) (*Task, error)
	UpdateTaskResult(ctx context.Context, id string, expectedUpdatedAt time.Time, result TaskResult) (*Task, error)
	ReconcileGhostTasks(ctx context.Context, alivePIDs []int) ([]Task, error)
	ReconcileStaleTasks(ctx context.Context, alivePIDs []int, staleThreshold time.Duration) ([]Task, error)
	ReconcileOrphanedQueued(ctx context.Context, minAge time.Duration) ([]Task, error)
	BlockTaskWithSubtasks(ctx context.Context, taskID string, expectedUpdatedAt time.Time, subtasks []DraftTask) (*Task, []Task, error)
	AppendTasksToProject(ctx context.Context, projectID, parentTaskID string, drafts []DraftTask) ([]Task, error)
	AddComment(ctx context.Context, c Comment) error
	ListComments(ctx context.Context, taskID string) ([]Comment, error)
	ListCommentsSince(ctx context.Context, taskID string, since time.Time) ([]Comment, error)
	ListUnprocessedHumanComments(ctx context.Context) ([]CommentRef, error)
	MarkCommentProcessed(ctx context.Context, taskID, commentEventID string) error
	AppendEvent(ctx context.Context, e Event) error
	ListEventsByTask(ctx context.Context, taskID string) ([]Event, error)
	MarkEventsCurated(ctx context.Context, taskID string) error
	DeleteCuratedEvents(ctx context.Context, taskID string) error
	ListCompletedTasksOlderThan(ctx context.Context, age time.Duration) ([]Task, error)
	RecordMemory(ctx context.Context, m Memory) error
	ListMemories(ctx context.Context, filter MemoryFilter) ([]Memory, error)
	RecallMemories(ctx context.Context, q RecallQuery) ([]Memory, error)
	TouchMemories(ctx context.Context, ids []string) error
	SupersedeMemories(ctx context.Context, oldIDs []string, newID string) error
	ListUnsupersededMemories(ctx context.Context) ([]Memory, error)
	GetAgentProfile(ctx context.Context, id string) (*AgentProfile, error)
	ListAgentProfiles(ctx context.Context) ([]AgentProfile, error)
	UpsertAgentProfile(ctx context.Context, p AgentProfile) error
	DeleteAgentProfile(ctx context.Context, id string) error
	AssignTaskAgent(ctx context.Context, taskID string, expectedUpdatedAt time.Time, agentID string) (*Task, error)
	ListSettings(ctx context.Context) ([]Setting, error)
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
	Close() error
}
