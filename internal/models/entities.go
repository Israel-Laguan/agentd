package models

import (
	"database/sql"
	"time"
)

// BaseEntity contains fields shared by persisted records.
type BaseEntity struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Project is a local workspace backed by a Kanban plan.
type Project struct {
	BaseEntity
	Name          string
	OriginalInput string
	WorkspacePath string
	Status        ProjectStatus
}

// Task is the durable unit of work moved by the Kanban state machine.
type Task struct {
	BaseEntity
	ProjectID     string
	AgentID       string
	Title         string
	Description   string
	State         TaskState
	Assignee      TaskAssignee
	OSProcessID   *int
	StartedAt     *time.Time
	CompletedAt   *time.Time
	LastHeartbeat *time.Time
	RetryCount    int
	TokenUsage    int
	DependsOn     []string
	Logs          string
}

// TaskResult is the durable outcome reported by a worker after running a task.
type TaskResult struct {
	Success bool
	Payload string
}

// TaskRelation models a dependency edge: parent must complete before child.
type TaskRelation struct {
	ParentTaskID string
	ChildTaskID  string
	RelationType TaskRelationType
}

// Comment records human or system context on a task.
type Comment struct {
	BaseEntity
	TaskID string
	Author CommentAuthor
	Body   string
	// Content is a proposal-aligned alias used by box-level contracts.
	Content string
	HasRead bool
}

// CommentRef identifies a human comment that still needs queue intake.
type CommentRef struct {
	TaskID         string
	CommentEventID string
	Body           string
	UpdatedAt      time.Time
}

// Event records observable task/project changes for the SSE stream and audit log.
type Event struct {
	BaseEntity
	ProjectID string
	TaskID    sql.NullString
	Type      EventType
	Payload   string
}

// AgentProfile configures a concrete model/provider pair.
type AgentProfile struct {
	ID           string
	Name         string
	Provider     string
	Model        string
	Temperature  float64
	SystemPrompt sql.NullString
	Role         string
	MaxTokens    int
	// AgenticMode enables agentic worker behavior, allowing the agent to
	// autonomously plan and execute multi-step tasks without human intervention.
	AgenticMode bool
	// InstructionsPath overrides the default project instructions file path
	// (e.g., ".agentd/AGENTS.md"). When empty, the loader uses the config default.
	InstructionsPath string
	// DryRun enables simulation mode. When true, tool calls return
	// synthesized results without executing the real handler.
	DryRun bool
	// Plugins lists plugin names activated for sessions using this
	// profile (session-scoped activation).
	Plugins   []string
	UpdatedAt time.Time
}

// Memory stores lessons learned globally or per project.
type Memory struct {
	ID             string
	Scope          MemoryScope
	ProjectID      sql.NullString
	Tags           sql.NullString
	Symptom        sql.NullString
	Solution       sql.NullString
	CreatedAt      time.Time
	LastAccessedAt sql.NullString
	AccessCount    int
	SupersededBy   sql.NullString
}

// MemoryFilter constrains memory lookups by scope and optional project.
type MemoryFilter struct {
	Scope     MemoryScope
	ProjectID sql.NullString
	Tags      []string
	Limit     int
	Offset    int
}

// RecallQuery describes a semantic memory lookup with namespace isolation.
type RecallQuery struct {
	Intent    string
	ProjectID string
	UserID    string
	Limit     int
}

// Setting is a persisted key/value configuration entry.
type Setting struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}
