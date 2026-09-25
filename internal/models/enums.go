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
	TaskStateNeedsContext        TaskState = "NEEDS_CONTEXT"
	TaskStateInConsideration     TaskState = "IN_CONSIDERATION"
)

// allTaskStates lists every task state agentd knows. Valid reports membership
// in this list, and the tasks.state CHECK constraint in the SQLite schema must
// accept exactly these values — a state the Go enum allows but the DB rejects
// fails at write time, which is how NEEDS_CONTEXT shipped broken in S04.
// internal/kanban.TestTaskStateCheckConstraintParity guards the pair.
// The slice is immutable; use AllTaskStatesSlice() to obtain a copy.
var allTaskStates = []TaskState{
	TaskStatePending,
	TaskStateReady,
	TaskStateQueued,
	TaskStateRunning,
	TaskStateBlocked,
	TaskStateCompleted,
	TaskStateFailed,
	TaskStateFailedRequiresHuman,
	TaskStateNeedsContext,
	TaskStateInConsideration,
}

// AllTaskStatesSlice returns a copy of the task state registry.
func AllTaskStatesSlice() []TaskState {
	return append([]TaskState(nil), allTaskStates...)
}

var validTaskTransitions = map[TaskState]map[TaskState]struct{}{
	TaskStatePending: {
		TaskStateReady:           {},
		TaskStateInConsideration: {},
		TaskStateFailed:          {},
		TaskStateNeedsContext:    {},
	},
	TaskStateReady: {
		TaskStateQueued:              {},
		TaskStateRunning:             {},
		TaskStateBlocked:             {},
		TaskStateInConsideration:     {},
		TaskStateFailed:              {},
		TaskStateFailedRequiresHuman: {},
		TaskStateNeedsContext:        {},
	},
	TaskStateQueued: {
		TaskStateRunning:             {},
		TaskStateReady:               {},
		TaskStateInConsideration:     {},
		TaskStateFailed:              {},
		TaskStateFailedRequiresHuman: {},
		TaskStateNeedsContext:        {},
	},
	TaskStateRunning: {
		TaskStateBlocked:             {},
		TaskStateCompleted:           {},
		TaskStateFailed:              {},
		TaskStateFailedRequiresHuman: {},
		TaskStateNeedsContext:        {},
		TaskStateReady:               {},
		TaskStateInConsideration:     {},
	},
	TaskStateBlocked: {
		TaskStateReady:               {},
		TaskStateInConsideration:     {},
		TaskStateFailed:              {},
		TaskStateFailedRequiresHuman: {},
	},
	TaskStateFailed: {
		TaskStateReady:               {},
		TaskStateInConsideration:     {},
		TaskStateFailedRequiresHuman: {},
	},
	// A tiered step can nominally COMPLETED (the model produced its
	// artifact) and only afterward be judged to have read from an
	// insufficient ContextPack — the NEEDS_CONTEXT re-gather is detected by
	// parsing that already-committed output, not before it.
	TaskStateCompleted: {
		TaskStateNeedsContext: {},
	},
	TaskStateFailedRequiresHuman: {
		TaskStateReady:           {},
		TaskStateInConsideration: {},
	},
	// A NEEDS_CONTEXT step is revived once a fresh ContextPack exists, or
	// abandoned if the re-gather itself cannot be scheduled.
	TaskStateNeedsContext: {
		TaskStateReady:               {},
		TaskStateInConsideration:     {},
		TaskStateFailed:              {},
		TaskStateFailedRequiresHuman: {},
	},
	TaskStateInConsideration: {
		TaskStatePending: {},
		TaskStateReady:   {},
		TaskStateFailed:  {},
	},
}

// Valid reports whether the state is known to agentd.
func (s TaskState) Valid() bool {
	for _, known := range allTaskStates {
		if known == s {
			return true
		}
	}
	return false
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

// VerifyResultOutcome classifies the result of a verify step.
type VerifyResultOutcome string

const (
	VerifyOutcomePass     VerifyResultOutcome = "pass"
	VerifyOutcomeFlake    VerifyResultOutcome = "flake"
	VerifyOutcomeFail     VerifyResultOutcome = "fail"
	VerifyOutcomeConflict VerifyResultOutcome = "conflict"
)

// Valid reports whether the verify outcome is known to agentd.
func (v VerifyResultOutcome) Valid() bool {
	switch v {
	case VerifyOutcomePass, VerifyOutcomeFlake, VerifyOutcomeFail, VerifyOutcomeConflict:
		return true
	default:
		return false
	}
}

// EventType classifies persisted events.
type EventType string

const (
	EventTypeComment                   EventType = "COMMENT"
	EventTypeCommentIntake             EventType = "COMMENT_INTAKE"
	EventTypeLog                       EventType = "LOG"
	EventTypeFailure                   EventType = "FAILURE"
	EventTypeResult                    EventType = "RESULT"
	EventTypeRecovery                  EventType = "RECOVERY"
	EventTypeRebootRecovery            EventType = "REBOOT_RECOVERY"
	EventTypeRebootRecoveryHandoff     EventType = "REBOOT_RECOVERY_HANDOFF"
	EventTypeHeartbeatReconcile        EventType = "HEARTBEAT_RECONCILE"
	EventTypeToolCall                  EventType = "TOOL_CALL"
	EventTypeToolResult                EventType = "TOOL_RESULT"
	EventTypeGoalStalled               EventType = "GOAL_STALLED"
	EventTypeTopicDrift                EventType = "TOPIC_DRIFT"
	EventTypeWarning                   EventType = "WARNING"
	EventTypeTokenUsage                EventType = "TOKEN_USAGE"
	EventTypePermissionDetected        EventType = "PERMISSION_DETECTED"
	EventTypePermissionHandoff         EventType = "PERMISSION_HANDOFF"
	EventTypeHumanResolution           EventType = "HUMAN_RESOLUTION"
	EventTypeTieredEscalationExhausted EventType = "TIERED_ESCALATION_EXHAUSTED"
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
