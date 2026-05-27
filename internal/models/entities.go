package models

import (
	"database/sql"
	"time"
)

// BaseEntity contains fields shared by persisted records.
type BaseEntity struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Project is a local workspace backed by a Kanban plan.
type Project struct {
	BaseEntity
	Name          string        `json:"name"`
	OriginalInput string        `json:"original_input"`
	WorkspacePath string        `json:"workspace_path"`
	Status        ProjectStatus `json:"status"`
}

// Task is the durable unit of work moved by the Kanban state machine.
type Task struct {
	BaseEntity
	ProjectID       string       `json:"project_id"`
	AgentID         string       `json:"agent_id"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	State           TaskState    `json:"state"`
	Assignee        TaskAssignee `json:"assignee"`
	OSProcessID     *int         `json:"os_process_id,omitempty"`
	StartedAt       *time.Time   `json:"started_at,omitempty"`
	CompletedAt     *time.Time   `json:"completed_at,omitempty"`
	LastHeartbeat   *time.Time   `json:"last_heartbeat,omitempty"`
	RetryCount      int          `json:"retry_count"`
	TokenUsage      int          `json:"token_usage"`
	SuccessCriteria []string     `json:"success_criteria"`
	DependsOn       []string     `json:"depends_on"`
	Logs            string       `json:"logs"`
}

// TaskResult is the durable outcome reported by a worker after running a task.
type TaskResult struct {
	Success bool   `json:"success"`
	Payload string `json:"payload"`
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
	TaskID string        `json:"task_id"`
	Author CommentAuthor `json:"author"`
	Body   string        `json:"body"`
	// Content is a proposal-aligned alias used by box-level contracts.
	Content string `json:"content"`
	HasRead bool   `json:"has_read"`
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
	// AgenticMode enables agentic worker behavior (tool round-tripping). Requires
	// Provider to name a tool-capable backend (openai or anthropic today). When
	// Provider is empty or unsupported, the worker logs a warning and falls back
	// to legacy single-shot JSON command execution.
	AgenticMode bool
	// InstructionsPath overrides the default project instructions file path
	// (e.g., ".agentd/AGENTS.md"). When empty, the loader uses the config default.
	InstructionsPath string
	// DryRun enables simulation mode. When true, tool calls return
	// synthesized results without executing the real handler.
	DryRun bool
	// RequireReview when true sends the task result for human review
	// before marking the task complete.
	RequireReview bool
	// GatedTools lists tool names that require human approval before
	// execution (e.g., "deploy", "write_config", "run_migration").
	GatedTools []string
	// Plugins lists plugin names activated for sessions using this
	// profile (session-scoped activation).
	Plugins []string
	// DisableTopicDrift when true skips topic drift detection for this profile.
	DisableTopicDrift bool
	// ToolManifestType forces a manifest category (e.g. "summarize", "code_gen").
	// Empty = use the keyword classifier when tool manifest is enabled.
	ToolManifestType string
	// CapabilityRouteIntent forces an external capability intent (e.g. "generate_image").
	// Empty = use the keyword classifier when capability routing is enabled.
	CapabilityRouteIntent string
	// AllowedTools, when non-empty, bypasses classifier and manifest; only listed
	// tools are advertised to the model.
	AllowedTools []string
	UpdatedAt         time.Time
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
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}
