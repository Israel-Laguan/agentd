package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/worker/agentic"
)

// Ensure *Worker implements agentic.Host
var _ agenticHost = (*Worker)(nil)

// We define a local alias of agentic.Host for the interface assertion to avoid circular dependency
// if agentic package imported worker (but agentic does NOT import worker).
// Worker imports agentic, so it can use agentic.Host directly.
type agenticHost interface {
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

func (w *Worker) FailHard(ctx context.Context, task models.Task, err error) {
	w.failHard(ctx, task, err)
}

func (w *Worker) RunLegacyTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, force bool) {
	w.runLegacyTask(ctx, task, project, profile, force)
}

func (w *Worker) RegisterCancel(taskID string, cancel context.CancelFunc) {
	w.registerCancel(taskID, cancel)
}

func (w *Worker) DeregisterCancel(taskID string) {
	w.deregisterCancel(taskID)
}

func (w *Worker) HandleGatewayError(ctx context.Context, task models.Task, err error) {
	w.handleGatewayError(ctx, task, err)
}

func (w *Worker) RecordTaskTokenUsage(ctx context.Context, task models.Task, tokens int) {
	w.recordTaskTokenUsage(ctx, task, tokens)
}

func (w *Worker) CommitTextWithProfile(ctx context.Context, task models.Task, text string, profile *models.AgentProfile) {
	w.commitTextWithProfile(ctx, task, text, profile)
}

func (w *Worker) DispatchToolWithHooks(
	ctx context.Context,
	sessionID, projectID, turnID string,
	taskUpdatedAt time.Time,
	call gateway.ToolCall,
	toolToAdapter map[string]string,
	toolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain,
	taskCaps *capabilities.Registry,
	providerName string,
) (agenttools.ToolResult, bool) {
	return w.dispatchToolWithHooks(ctx, sessionID, projectID, turnID, taskUpdatedAt, call, toolToAdapter, toolExecutor, taskHooks, taskCaps, providerName)
}

func (w *Worker) RunPreTaskElicitation(ctx context.Context, task models.Task, project models.Project) (models.Task, bool, error) {
	return w.runPreTaskElicitation(ctx, task, project)
}

func (w *Worker) Emit(ctx context.Context, task models.Task, kind, payload string) {
	w.emit(ctx, task, kind, payload)
}

func (w *Worker) AssembleAgenticSystemPrompt(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	return w.assembleAgenticSystemPrompt(ctx, task, project, profile)
}

func (w *Worker) AssembleAgenticSystemPromptWithUserContent(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	userContent string,
) []gateway.PromptMessage {
	return w.assembleAgenticSystemPromptWithUserContent(ctx, task, project, profile, userContent)
}

func (w *Worker) PrependReviewRejectionFeedback(ctx context.Context, task models.Task, messages []gateway.PromptMessage) ([]gateway.PromptMessage, string) {
	return w.prependReviewRejectionFeedback(ctx, task, messages)
}

func (w *Worker) ApplyModelRouting(task models.Task, profile models.AgentProfile, messages []gateway.PromptMessage, tools []gateway.ToolDefinition) models.AgentProfile {
	return w.applyModelRouting(task, profile, messages, tools)
}

func (w *Worker) ApplyTuning(req gateway.AIRequest, task models.Task, profile models.AgentProfile, sessionRecoveryGen int) gateway.AIRequest {
	return w.applyTuning(req, task, profile, sessionRecoveryGen)
}

func (w *Worker) MountAgenticHooks(project models.Project, profile models.AgentProfile) (*agenthooks.HookChain, *capabilities.Registry) {
	taskHooks, taskCaps := w.mountScopedPlugins(project, profile)
	if len(profile.GatedTools) > 0 {
		if taskHooks == nil {
			taskHooks = agenthooks.NewHookChain()
		}
		handler := NewBlockingApprovalHandler(w.store)
		taskHooks.RegisterPre(ApprovalGateHook(profile.GatedTools, handler))
	}
	return taskHooks, taskCaps
}

func (w *Worker) AgenticToolsWithExtras(ctx context.Context, executor *agenttools.ToolExecutor, caps *capabilities.Registry) ([]gateway.ToolDefinition, map[string]string) {
	return w.agenticToolsWithExtras(ctx, executor, caps)
}

func (w *Worker) FilterAgenticTools(tools []gateway.ToolDefinition, toolToAdapter map[string]string, task models.Task, profile models.AgentProfile) ([]gateway.ToolDefinition, map[string]string) {
	return w.filterAgenticTools(tools, toolToAdapter, task, profile)
}

func (w *Worker) GeneratePlan(ctx context.Context, task models.Task, project models.Project, budget *agentruntime.BudgetGuard) (*agentcontext.Plan, error) {
	return w.generatePlan(ctx, task, project, budget)
}

func (w *Worker) InjectPlan(messages []gateway.PromptMessage, plan *agentcontext.Plan) []gateway.PromptMessage {
	return w.injectPlan(messages, plan)
}

func (w *Worker) ShouldPlanWithBudget(task models.Task, budget *agentruntime.BudgetGuard) bool {
	return w.shouldPlanWithBudget(task, budget)
}

func (w *Worker) RepairOutputWithPlan(ctx context.Context, task models.Task, plan *agentcontext.Plan, content string, budget *agentruntime.BudgetGuard) (string, bool) {
	return w.repairOutputWithPlan(ctx, task, plan, content, budget)
}

func (w *Worker) GenerateRespecifiedUserTurn(
	ctx context.Context,
	task models.Task,
	plan *agentcontext.Plan,
	failing []agentcontext.PlanStep,
	messages []gateway.PromptMessage,
	cm *agentcontext.ContextManager,
	budget *agentruntime.BudgetGuard,
) (string, error) {
	return w.generateRespecifiedUserTurn(ctx, task, plan, failing, messages, cm, budget)
}

func (w *Worker) RunSessionStart(ctx context.Context, task models.Task, project models.Project) error {
	if w.hooks == nil {
		return nil
	}
	return w.hooks.RunSessionStart(agenthooks.HookContext{
		SessionID: task.ID,
		ProjectID: project.ID,
		Timestamp: time.Now(),
		ExecCtx:   ctx,
	})
}

func encodeCapabilityResult(out any) (string, error) {
	if s, ok := out.(string); ok {
		return s, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("capability result encode: %w", err)
	}
	return string(encoded), nil
}

func (w *Worker) commitCapabilityRouteResult(
	ctx context.Context,
	task models.Task,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
	text string,
) agentruntime.LoopResult {
	if w.messageEditor != nil {
		w.messageEditor.Commit(messages, gateway.PromptMessage{
			Role:    "assistant",
			Content: text,
		})
	} else if messages != nil {
		*messages = append(*messages, gateway.PromptMessage{
			Role:    "assistant",
			Content: text,
		})
	}
	w.commitTextWithProfile(ctx, task, text, &profile)
	return agentruntime.LoopResult{Status: agentruntime.LoopSuccessfulCompletion}
}

func (w *Worker) TryExternalCapabilityRoute(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
) (agentruntime.LoopResult, bool, error) {
	_ = project
	if w.capabilityRouter == nil {
		return agentruntime.LoopResult{}, false, nil
	}

	decision, ok := w.capabilityRouter.Route(task, profile)
	if !ok {
		return agentruntime.LoopResult{}, false, nil
	}

	if w.capabilities == nil {
		slog.Warn("capability routing: no capability registry; falling through to agentic loop",
			"task_id", task.ID,
			"intent", decision.Intent,
			"adapter", decision.Adapter,
		)
		return agentruntime.LoopResult{}, false, nil
	}

	adapter, found := w.capabilities.GetAdapter(decision.Adapter)
	if !found || adapter == nil {
		slog.Warn("capability routing: adapter not registered; falling through to agentic loop",
			"task_id", task.ID,
			"intent", decision.Intent,
			"adapter", decision.Adapter,
		)
		return agentruntime.LoopResult{}, false, nil
	}

	out, err := w.capabilities.CallTool(ctx, decision.Adapter, decision.Tool, decision.Args)
	if err != nil {
		routeErr := fmt.Errorf("capability routing: %s/%s: %w", decision.Adapter, decision.Tool, err)
		w.failHard(ctx, task, routeErr)
		return agentruntime.LoopResult{}, false, routeErr
	}

	text, err := encodeCapabilityResult(out)
	if err != nil {
		w.failHard(ctx, task, err)
		return agentruntime.LoopResult{}, false, err
	}

	return w.commitCapabilityRouteResult(ctx, task, profile, messages, text), true, nil
}

func (w *Worker) RecordTurnSnapshot(
	sessionID, projectID, provider, turnID string,
	messageCount, tokenCount int,
	activeTools []string,
	goalProgress float64,
) {
	w.recordTurnSnapshot(sessionID, projectID, provider, turnID, messageCount, tokenCount, activeTools, goalProgress)
}

func (w *Worker) RecordLoopResult(result agentruntime.LoopResult) {
	w.recordLoopResult(result)
}

func (w *Worker) HandleGoalStalled(ctx context.Context, task models.Task, gt *agentcontext.GoalTracker) error {
	return w.handleGoalStalled(ctx, task, gt)
}

func (w *Worker) processAgentic(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (LoopResult, bool) {
	engine := agentic.NewEngine(agentic.Config{
		Store:                   w.store,
		Gateway:                 w.gateway,
		Sandbox:                 w.sandbox,
		SandboxEnvAllowlist:     w.sandboxEnvAllowlist,
		SandboxExtraEnv:         w.sandboxExtraEnv,
		SandboxWallTimeout:      w.sandboxWallTimeout,
		FileContextCfg:          w.fileContextCfg,
		DocStore:                w.docStore,
		ContextCfg:              w.contextCfg,
		MaxToolIterations:       w.maxToolIterations,
		BudgetTracker:           w.budgetTracker,
		ContextWarningThreshold: w.contextWarningThreshold,
		ToolFailureStreak:       w.toolFailureStreak,
		TruncatorMax:            w.truncatorMax,
		CharacterBudget:         w.characterBudget,
		PlanningCfg:             w.planningCfg,
		MessageEditor:           w.messageEditor,
		CheckpointStore:         w.checkpointStore,
		TopicGuard:              w.topicGuard,
		ModelRouter:             w.modelRouter,
		Capabilities:            w.capabilities,
	}, w)
	return engine.Process(ctx, task, project, profile)
}

