package agentic

import (
	"context"
	"time"

	agentcontext "agentd/internal/agent/context"
	wfilecontext "agentd/internal/agent/filecontext"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type Config struct {
	Store                   TaskReadStore
	Gateway                 gateway.AIGateway
	Sandbox                 sandbox.Executor
	SandboxEnvAllowlist     []string
	SandboxExtraEnv         []string
	SandboxWallTimeout      time.Duration
	FileContextCfg          config.FileContextConfig
	DocStore                *wfilecontext.DocStore
	ContextCfg              config.AgenticContextConfig
	ContextTruncation       gateway.TruncationStrategy
	MaxToolIterations       int
	BudgetTracker           spec.BudgetTracker
	ContextWarningThreshold float64
	ToolFailureStreak       int
	TruncatorMax            int
	CharacterBudget         int
	PlanningCfg             config.AgenticPlanningConfig
	MessageEditor           *agentcontext.MessageEditor
	CheckpointStore         wsession.CheckpointStore
	TopicGuard              *agentruntime.TopicGuard
	Capabilities            *capabilities.Registry
	// CriteriaMet saves the success criteria a turn met. It reports progress
	// only; the terminal outcome goes through Result.
	CriteriaMet func(ctx context.Context, taskID string, met []string) error
}

type TaskReadStore interface {
	GetTask(ctx context.Context, taskID string) (*models.Task, error)
	ListCommentsSince(ctx context.Context, taskID string, since time.Time) ([]models.Comment, error)
}

// PromptHang is a bash call that stopped at an interactive prompt.
type PromptHang struct {
	// Command is the command as the model sent it; a repeat of it hands off.
	Command string
	// Payload is the PROMPT_DETECTED payload.
	Payload string
	// Feedback is the corrective tool result the model gets on the first hang.
	Feedback string
}

// Host is what the engine calls into. Most terminal outcomes go through Result
// and the worker applies them (T-041); HandleGoalStalled still blocks the task
// directly.
type Host interface {
	RegisterCancel(taskID string, cancel context.CancelFunc)
	DeregisterCancel(taskID string)
	// RecordProviderSuccess closes the task's provider breaker. The engine calls
	// it only after a turn's LLM request actually succeeded, so a turn that ends
	// locally (tool failure, budget exhaustion, break-up) never closes an OPEN
	// circuit the provider has not recovered from.
	RecordProviderSuccess(ctx context.Context, task models.Task)
	RecordTaskTokenUsage(ctx context.Context, task models.Task, tokens int, details spec.UsageDetails)
	DispatchToolWithHooks(
		ctx context.Context,
		sessionID, projectID, turnID string,
		taskUpdatedAt time.Time,
		call gateway.ToolCall,
		toolToAdapter map[string]string,
		toolExecutor *agenttools.ToolExecutor,
		taskHooks *agenthooks.HookChain,
		taskCaps *capabilities.Registry,
		providerName string,
	) (agenttools.ToolResult, bool)
	// AgenticPromptHang reports a bash call that timed out with an interactive
	// prompt in its output.
	AgenticPromptHang(call gateway.ToolCall, result agenttools.ToolResult) (PromptHang, bool)
	AgenticPrivilegeBlock(call gateway.ToolCall, result agenttools.ToolResult) (string, bool)
	RecoverAgenticPrivilege(
		ctx context.Context,
		task models.Task,
		profile models.AgentProfile,
		command string,
		blockedResult agenttools.ToolResult,
		allowAlternative bool,
		dispatch func(gateway.ToolCall) (agenttools.ToolResult, bool),
	) (agenttools.ToolResult, bool)
	Emit(ctx context.Context, task models.Task, kind, payload string)
	AssembleAgenticSystemPrompt(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage
	AssembleAgenticSystemPromptWithUserContent(
		ctx context.Context,
		task models.Task,
		project models.Project,
		profile models.AgentProfile,
		userContent string,
	) []gateway.PromptMessage
	PrependReviewRejectionFeedback(ctx context.Context, task models.Task, messages []gateway.PromptMessage) ([]gateway.PromptMessage, string)
	// PrependSandboxViolationFeedback tells the model which earlier commands the
	// sandbox refused, so a retry does not repeat them.
	PrependSandboxViolationFeedback(ctx context.Context, task models.Task, messages []gateway.PromptMessage) []gateway.PromptMessage
	ApplyTuning(req gateway.AIRequest, task models.Task, profile models.AgentProfile, sessionRecoveryGen int) gateway.AIRequest
	AgenticToolsWithExtras(ctx context.Context, executor *agenttools.ToolExecutor, caps *capabilities.Registry) ([]gateway.ToolDefinition, map[string]string)
	FilterAgenticTools(tools []gateway.ToolDefinition, toolToAdapter map[string]string, task models.Task, profile models.AgentProfile) ([]gateway.ToolDefinition, map[string]string)
	GeneratePlan(ctx context.Context, task models.Task, project models.Project, budget *agentruntime.BudgetGuard) (*agentcontext.Plan, error)
	InjectPlan(messages []gateway.PromptMessage, plan *agentcontext.Plan) []gateway.PromptMessage
	ShouldPlanWithBudget(task models.Task, budget *agentruntime.BudgetGuard) bool
	RepairOutputWithPlan(ctx context.Context, task models.Task, plan *agentcontext.Plan, content string, budget *agentruntime.BudgetGuard) (string, bool)
	GenerateRespecifiedUserTurn(
		ctx context.Context,
		task models.Task,
		plan *agentcontext.Plan,
		failing []agentcontext.PlanStep,
		messages []gateway.PromptMessage,
		cm *agentcontext.ContextManager,
		budget *agentruntime.BudgetGuard,
	) (string, error)
	// RecordAttemptFailure remembers the first failing shell command of this
	// attempt for the next attempt to check (B-043).
	RecordAttemptFailure(task models.Task, call gateway.ToolCall, result agenttools.ToolResult)
	// RepeatedAttemptFailure returns the refusal feedback for a bash call that
	// repeats the command that failed first in the previous attempt (B-043).
	RepeatedAttemptFailure(task models.Task, call gateway.ToolCall) (string, bool)
	// VerifyPostConditions checks the success-criteria files a task names
	// before a text-only finish is committed (B-039).
	VerifyPostConditions(task models.Task, project models.Project) error
	// VerifyBreakdownComplete returns an error while the task's breakdown
	// subtasks are not all completed, so a text-only finish cannot complete the
	// parent early (T-055b gap 2). Open subtasks wrap
	// models.ErrBreakdownIncomplete; any other error is a failed read.
	VerifyBreakdownComplete(ctx context.Context, task models.Task) error
	RecordTurnSnapshot(
		sessionID, projectID, provider, turnID string,
		messageCount, tokenCount int,
		activeTools []string,
		goalProgress float64,
	)
	RecordLoopResult(result agentruntime.LoopResult)
	HandleGoalStalled(ctx context.Context, task models.Task, gt *agentcontext.GoalTracker) error
}

type Engine struct {
	config                        Config
	host                          Host
	privilegeAlternativeAttempted bool
	// repeats spots the same tool call returning the same result again and
	// again in this task run (B-081); created on first use.
	repeats *agenttools.RepeatTracker
	// lastBashFailure is the failing result of the most recent shell command
	// that did not pass, and "" once a later command passes (B-082).
	lastBashFailure string
	// evidence counts what this run did to the workspace: commands that passed
	// and files written. A text-only finish needs some of it to be COMPLETED.
	evidence runEvidence
	// result is the terminal outcome this run settled on, if any (see Result).
	// Every settling path ends the loop, so the last write is the only one.
	result Result
	// hungCommands holds the commands that already stopped at a prompt in this
	// run; a repeat of one hands the task to a human. Created on first use.
	hungCommands map[string]bool
}

func NewEngine(cfg Config, host Host) *Engine {
	return &Engine{
		config: cfg,
		host:   host,
	}
}
