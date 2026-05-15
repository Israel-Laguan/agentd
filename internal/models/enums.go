package models

import "strings"

// TaskState is the persisted lifecycle state for a Kanban task.
type TaskState string

const (
	TaskStatePending             TaskState = "PENDING"
	TaskStateReady               TaskState = "READY"
	TaskStateQueued              TaskState = "QUEUED"
	TaskStateRunning             TaskState = "RUNNING"
	TaskStateBlocked             TaskState = "BLOCKED"
	TaskStateCompleted           TaskState = "COMPLETED"
	TaskStateFailed              TaskState = "FAILED"
	TaskStateFailedRequiresHuman TaskState = "FAILED_REQUIRES_HUMAN"
	TaskStateInConsideration     TaskState = "IN_CONSIDERATION"
)

var validTaskTransitions = map[TaskState]map[TaskState]struct{}{
	TaskStatePending: {
		TaskStateReady:           {},
		TaskStateInConsideration: {},
		TaskStateFailed:          {},
	},
	TaskStateReady: {
		TaskStateQueued:              {},
		TaskStateRunning:             {},
		TaskStateBlocked:             {},
		TaskStateInConsideration:     {},
		TaskStateFailed:              {},
		TaskStateFailedRequiresHuman: {},
	},
	TaskStateQueued: {
		TaskStateRunning:             {},
		TaskStateReady:               {},
		TaskStateInConsideration:     {},
		TaskStateFailed:              {},
		TaskStateFailedRequiresHuman: {},
	},
	TaskStateRunning: {
		TaskStateBlocked:             {},
		TaskStateCompleted:           {},
		TaskStateFailed:              {},
		TaskStateFailedRequiresHuman: {},
		TaskStateReady:               {},
		TaskStateInConsideration:     {},
	},
	TaskStateBlocked: {
		TaskStateReady:           {},
		TaskStateInConsideration: {},
		TaskStateFailed:          {},
	},
	TaskStateFailed: {
		TaskStateReady:               {},
		TaskStateInConsideration:     {},
		TaskStateFailedRequiresHuman: {},
	},
	TaskStateFailedRequiresHuman: {
		TaskStateReady:           {},
		TaskStateInConsideration: {},
	},
	TaskStateInConsideration: {
		TaskStatePending: {},
		TaskStateReady:   {},
		TaskStateFailed:  {},
	},
}

// Valid reports whether the state is known to agentd.
func (s TaskState) Valid() bool {
	switch s {
	case TaskStatePending, TaskStateReady, TaskStateQueued, TaskStateRunning, TaskStateBlocked, TaskStateCompleted, TaskStateFailed, TaskStateFailedRequiresHuman, TaskStateInConsideration:
		return true
	default:
		return false
	}
}

// CanTransitionTo enforces the v1 task state machine.
func (s TaskState) CanTransitionTo(next TaskState) bool {
	if !s.Valid() || !next.Valid() {
		return false
	}
	_, ok := validTaskTransitions[s][next]
	return ok
}

// TaskAssignee identifies who currently owns action on a task.
type TaskAssignee string

const (
	TaskAssigneeSystem TaskAssignee = "SYSTEM"
	TaskAssigneeHuman  TaskAssignee = "HUMAN"
)

// Valid reports whether the assignee is known to agentd.
func (a TaskAssignee) Valid() bool {
	switch a {
	case TaskAssigneeSystem, TaskAssigneeHuman:
		return true
	default:
		return false
	}
}

// ProjectStatus describes project lifecycle state.
type ProjectStatus string

const (
	ProjectStatusActive    ProjectStatus = "ACTIVE"
	ProjectStatusCompleted ProjectStatus = "COMPLETED"
	ProjectStatusArchived  ProjectStatus = "ARCHIVED"
)

// Valid reports whether the project status is known to agentd.
func (s ProjectStatus) Valid() bool {
	switch s {
	case ProjectStatusActive, ProjectStatusCompleted, ProjectStatusArchived:
		return true
	default:
		return false
	}
}

// TaskRelationType identifies why an edge exists in the task graph.
type TaskRelationType string

const (
	TaskRelationBlocks    TaskRelationType = "BLOCKS"
	TaskRelationSpawnedBy TaskRelationType = "SPAWNED_BY"
	TaskRelationDependsOn TaskRelationType = "DEPENDS_ON"
)

// Valid reports whether the relation type is known to agentd.
func (r TaskRelationType) Valid() bool {
	switch r {
	case TaskRelationBlocks, TaskRelationSpawnedBy, TaskRelationDependsOn:
		return true
	default:
		return false
	}
}

// EventType classifies persisted events.
type EventType string

const (
	EventTypeComment               EventType = "COMMENT"
	EventTypeCommentIntake         EventType = "COMMENT_INTAKE"
	EventTypeLog                   EventType = "LOG"
	EventTypeFailure               EventType = "FAILURE"
	EventTypeResult                EventType = "RESULT"
	EventTypeRecovery              EventType = "RECOVERY"
	EventTypeRebootRecovery        EventType = "REBOOT_RECOVERY"
	EventTypeRebootRecoveryHandoff EventType = "REBOOT_RECOVERY_HANDOFF"
	EventTypeHeartbeatReconcile    EventType = "HEARTBEAT_RECONCILE"
	EventTypeToolCall              EventType = "TOOL_CALL"
	EventTypeToolResult            EventType = "TOOL_RESULT"
	EventTypeGoalStalled           EventType = "GOAL_STALLED"
)

// CommentAuthor identifies the actor that produced a comment.
type CommentAuthor string

const (
	CommentAuthorUser        CommentAuthor = "USER"
	CommentAuthorFrontdesk   CommentAuthor = "FRONTDESK"
	CommentAuthorWorkerAgent CommentAuthor = "WORKER_AGENT"
)

// NormalizeCommentAuthor maps legacy casing/aliases to canonical values.
func NormalizeCommentAuthor(input string) CommentAuthor {
	normalized := strings.ToUpper(strings.TrimSpace(input))
	switch normalized {
	case "HUMAN", "USER":
		return CommentAuthorUser
	case "FRONTDESK":
		return CommentAuthorFrontdesk
	case "WORKER_AGENT", "SYSTEM":
		return CommentAuthorWorkerAgent
	default:
		return CommentAuthor(normalized)
	}
}

// MemoryScope constrains visibility of learned memories.
type MemoryScope string

const (
	MemoryScopeGlobal       MemoryScope = "GLOBAL"
	MemoryScopeProject      MemoryScope = "PROJECT"
	MemoryScopeTaskCuration MemoryScope = "TASK_CURATION"
	MemoryScopeUserPref     MemoryScope = "USER_PREFERENCE"
)

// Valid reports whether the memory scope is known to agentd.
func (s MemoryScope) Valid() bool {
	switch s {
	case MemoryScopeGlobal, MemoryScopeProject, MemoryScopeTaskCuration, MemoryScopeUserPref:
		return true
	default:
		return false
	}
}
