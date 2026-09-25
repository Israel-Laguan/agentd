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

type HumanHandoffResolver interface {
	ResolveHumanHandoff(ctx context.Context, taskID string, expectedUpdatedAt *time.Time, result string) (*HumanHandoffResolution, error)
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
	UpdateTaskDescription(ctx context.Context, id string, expectedUpdatedAt time.Time, description string) (*Task, error)
	// UpdateTaskPatch applies an optional state transition and optional description change in a
	// single atomic transaction. Either field may be nil to leave it unchanged. When both are
	// provided the operation is all-or-nothing: a failure on either write leaves the task row
	// untouched.
	UpdateTaskPatch(ctx context.Context, id string, expectedUpdatedAt time.Time, state *TaskState, description *string) (*Task, error)
	UpdateTaskResult(ctx context.Context, id string, expectedUpdatedAt time.Time, result TaskResult) (*Task, error)
	// CompleteTieredOrigin resolves a tiered pipeline origin straight from
	// BLOCKED, READY, or RUNNING to COMPLETED/FAILED in a single atomic
	// write (SP-006): unlike the BLOCKED→READY→RUNNING ladder + generic
	// UpdateTaskResult, it never exposes a transient READY state another
	// dispatcher could claim. The generic UpdateTaskState machine still
	// rejects BLOCKED/READY → COMPLETED; only this path may bypass it.
	CompleteTieredOrigin(ctx context.Context, id string, expectedUpdatedAt time.Time, result TaskResult) (*Task, error)
	UpdateCriteriaMet(ctx context.Context, id string, met []string) error
	ReconcileGhostTasks(ctx context.Context, alivePIDs []int) ([]Task, error)
	ReconcileStaleTasks(ctx context.Context, alivePIDs []int, staleThreshold time.Duration) ([]Task, error)
	ReconcileOrphanedQueued(ctx context.Context, minAge time.Duration) ([]Task, error)
	BlockTaskWithSubtasks(ctx context.Context, taskID string, expectedUpdatedAt time.Time, subtasks []DraftTask) (*Task, []Task, error)
	ListChildTasks(ctx context.Context, parentID string) ([]Task, error)
	ListChildTasksByRelation(ctx context.Context, parentID string, relationType TaskRelationType) ([]Task, error)
	ListParentTasks(ctx context.Context, childID string) ([]Task, error)
	// ListParentTasksByRelation returns parent tasks connected by a specific
	// relation type (e.g. SPAWNED_BY). Used by the tiered pipeline to
	// resolve the origin task without ambiguity.
	ListParentTasksByRelation(ctx context.Context, childID string, relationType TaskRelationType) ([]Task, error)
	ReconcileExpiredBlockedTasks(ctx context.Context, now time.Time) ([]Task, error)
	// PersistTieredDAG atomically blocks the parent and inserts pre-built
	// tiered step children with SPAWNED_BY and DEPENDS_ON relations.
	PersistTieredDAG(ctx context.Context, parentID string, expectedParentUpdatedAt time.Time, children []TieredDAGTask) ([]Task, error)
	// SpawnTieredContinuation inserts one or more child tasks onto an
	// already-BLOCKED tiered pipeline origin, wiring SPAWNED_BY (for
	// tryDispatchTieredStep's origin lookup) and, when a task's DependsOnID
	// is set, a DEPENDS_ON edge to that predecessor. Used by the escalation
	// ladder (mid-fix redo, strong-model escalation) and NEEDS_CONTEXT
	// re-gather, all of which spawn after the initial DAG split, when the
	// origin is no longer RUNNING/READY (PersistTieredDAG's precondition).
	SpawnTieredContinuation(ctx context.Context, originID string, children []TieredContinuationTask) ([]Task, error)
	// RewireDependsOn redirects any task with a DEPENDS_ON edge to
	// oldParentID onto newParentID instead, but only for dependents still
	// PENDING or READY (a QUEUED/RUNNING dependent is left alone — it is
	// already in flight against the old artifact and is allowed to finish,
	// per the tiered NEEDS_CONTEXT re-gather contract). A READY dependent is
	// additionally transitioned to BLOCKED so it cannot run against the
	// stale predecessor while the new one is still in progress. Returns the
	// rewired tasks.
	RewireDependsOn(ctx context.Context, oldParentID, newParentID string) ([]Task, error)
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
	// MarkProjectTasksReady transitions all PENDING tasks belonging to the
	// given project to READY. Used to unlock tasks after workspace seeding.
	MarkProjectTasksReady(ctx context.Context, projectID string) ([]Task, error)
	ListSettings(ctx context.Context) ([]Setting, error)
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
	Close() error
}

// ScheduledTaskStore persists the scheduler registry and creates dispatch tasks.
type ScheduledTaskStore interface {
	ListScheduledTasks(ctx context.Context) ([]ScheduledTask, error)
	UpsertScheduledTask(ctx context.Context, t ScheduledTask) error
	DeleteScheduledTask(ctx context.Context, id string) error
	UpdateScheduledTaskLastFired(ctx context.Context, id string, firedAt time.Time) error
	ScheduleDeferredRequeue(ctx context.Context, taskID string, runAfter time.Time) error
	InsertReadyTask(ctx context.Context, projectID string, draft DraftTask) (*Task, error)
	InsertReadyTaskAndRecordDispatch(ctx context.Context, projectID string, draft DraftTask, scheduleID string, slot time.Time, deleteEntry bool) (*Task, error)
	EnsureSystemProject(ctx context.Context) (*Project, error)
	GetTask(ctx context.Context, id string) (*Task, error)
	UpdateTaskState(ctx context.Context, id string, expectedUpdatedAt time.Time, next TaskState) (*Task, error)
}
