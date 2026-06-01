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
	Store                   models.KanbanStore
	Gateway                 gateway.AIGateway
	Sandbox                 sandbox.Executor
	SandboxEnvAllowlist     []string
	SandboxExtraEnv         []string
	SandboxWallTimeout      time.Duration
	FileContextCfg          config.FileContextConfig
	DocStore                *wfilecontext.DocStore
	ContextCfg              config.AgenticContextConfig
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
	ModelRouter             *agentruntime.ModelRouter
	Capabilities            *capabilities.Registry
}

type Host interface {
	FailHard(ctx context.Context, task models.Task, err error)
	RunLegacyTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, force bool)
	RegisterCancel(taskID string, cancel context.CancelFunc)
	DeregisterCancel(taskID string)
	HandleGatewayError(ctx context.Context, task models.Task, err error)
	RecordTaskTokenUsage(ctx context.Context, task models.Task, tokens int)
	CommitTextWithProfile(ctx context.Context, task models.Task, text string, profile *models.AgentProfile)
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
	RunPreTaskElicitation(ctx context.Context, task models.Task, project models.Project) (models.Task, bool, error)
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
	ApplyModelRouting(task models.Task, profile models.AgentProfile, messages []gateway.PromptMessage, tools []gateway.ToolDefinition) models.AgentProfile
	ApplyTuning(req gateway.AIRequest, task models.Task, profile models.AgentProfile, sessionRecoveryGen int) gateway.AIRequest
	MountAgenticHooks(project models.Project, profile models.AgentProfile) (*agenthooks.HookChain, *capabilities.Registry)
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
	RunSessionStart(ctx context.Context, task models.Task, project models.Project) error
	TryExternalCapabilityRoute(
		ctx context.Context,
		task models.Task,
		project models.Project,
		profile models.AgentProfile,
		messages *[]gateway.PromptMessage,
	) (agentruntime.LoopResult, bool, error)
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
	config Config
	host   Host
}

func NewEngine(cfg Config, host Host) *Engine {
	return &Engine{
		config: cfg,
		host:   host,
	}
}
