package worker

import (
	"context"
	"time"

	agenthooks "agentd/internal/agent/hooks"
	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/worker/agentic"
)

var _ agentic.Host = (*Worker)(nil)

// FailHard is the public Host interface method.
func (w *Worker) FailHard(ctx context.Context, task models.Task, err error) {
	w.failHard(ctx, task, err)
}

// RunLegacyTask is the public Host interface method.
func (w *Worker) RunLegacyTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, force bool) {
	w.runLegacyTask(ctx, task, project, profile, force)
}

// RegisterCancel is the public Host interface method.
func (w *Worker) RegisterCancel(taskID string, cancel context.CancelFunc) {
	w.registerCancel(taskID, cancel)
}

// DeregisterCancel is the public Host interface method.
func (w *Worker) DeregisterCancel(taskID string) {
	w.deregisterCancel(taskID)
}

// HandleGatewayError is the public Host interface method.
func (w *Worker) HandleGatewayError(ctx context.Context, task models.Task, err error) {
	w.handleGatewayError(ctx, task, err)
}

// RecordTaskTokenUsage is the public Host interface method.
func (w *Worker) RecordTaskTokenUsage(ctx context.Context, task models.Task, tokens int) {
	w.recordTaskTokenUsage(ctx, task, tokens)
}

// CommitTextWithProfile is the public Host interface method.
func (w *Worker) CommitTextWithProfile(ctx context.Context, task models.Task, text string, profile *models.AgentProfile) {
	w.commitTextWithProfile(ctx, task, text, profile)
}

// ApplyModelRouting is the public Host interface method.
func (w *Worker) ApplyModelRouting(
	task models.Task,
	profile models.AgentProfile,
	messages []gateway.PromptMessage,
	tools []gateway.ToolDefinition,
) models.AgentProfile {
	return w.applyModelRouting(task, profile, messages, tools)
}

// ApplyTuning is the public Host interface method.
func (w *Worker) ApplyTuning(req gateway.AIRequest, task models.Task, profile models.AgentProfile, sessionRecoveryGen int) gateway.AIRequest {
	return w.applyTuning(req, task, profile, sessionRecoveryGen)
}

// MountAgenticHooks is the public Host interface method.
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

// AgenticToolsWithExtras is the public Host interface method.
func (w *Worker) AgenticToolsWithExtras(ctx context.Context, executor *agenttools.ToolExecutor, caps *capabilities.Registry) ([]gateway.ToolDefinition, map[string]string) {
	return w.agenticToolsWithExtras(ctx, executor, caps)
}

// FilterAgenticTools is the public Host interface method.
func (w *Worker) FilterAgenticTools(tools []gateway.ToolDefinition, toolToAdapter map[string]string, task models.Task, profile models.AgentProfile) ([]gateway.ToolDefinition, map[string]string) {
	return w.filterAgenticTools(tools, toolToAdapter, task, profile)
}

// GeneratePlan is the public Host interface method.
func (w *Worker) GeneratePlan(ctx context.Context, task models.Task, project models.Project, budget *agentruntime.BudgetGuard) (*agentcontext.Plan, error) {
	return w.generatePlan(ctx, task, project, budget)
}

// InjectPlan is the public Host interface method.
func (w *Worker) InjectPlan(messages []gateway.PromptMessage, plan *agentcontext.Plan) []gateway.PromptMessage {
	return w.injectPlan(messages, plan)
}

// ShouldPlanWithBudget is the public Host interface method.
func (w *Worker) ShouldPlanWithBudget(task models.Task, budget *agentruntime.BudgetGuard) bool {
	return w.shouldPlanWithBudget(task, budget)
}

// RepairOutputWithPlan is the public Host interface method.
func (w *Worker) RepairOutputWithPlan(ctx context.Context, task models.Task, plan *agentcontext.Plan, content string, budget *agentruntime.BudgetGuard) (string, bool) {
	return w.repairOutputWithPlan(ctx, task, plan, content, budget)
}

// GenerateRespecifiedUserTurn is the public Host interface method.
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

// RunSessionStart is the public Host interface method.
func (w *Worker) RunSessionStart(ctx context.Context, task models.Task, project models.Project, taskHooks *agenthooks.HookChain) error {
	// Run worker-level hooks first.
	if w.hooks != nil {
		if err := w.hooks.RunSessionStart(agenthooks.HookContext{
			SessionID: task.ID,
			ProjectID: project.ID,
			Timestamp: time.Now(),
			ExecCtx:   ctx,
		}); err != nil {
			return err
		}
	}
	// Then run task-scoped hooks from scoped plugins.
	if taskHooks != nil {
		return taskHooks.RunSessionStart(agenthooks.HookContext{
			SessionID: task.ID,
			ProjectID: project.ID,
			Timestamp: time.Now(),
			ExecCtx:   ctx,
		})
	}
	return nil
}

// DispatchToolWithHooks is the public Host interface method.
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

// RunPreTaskElicitation is the public Host interface method.
func (w *Worker) RunPreTaskElicitation(ctx context.Context, task models.Task, project models.Project) (models.Task, bool, error) {
	return w.runPreTaskElicitation(ctx, task, project)
}

// Emit is the public Host interface method.
func (w *Worker) Emit(ctx context.Context, task models.Task, kind, payload string) {
	w.emit(ctx, task, kind, payload)
}

// AssembleAgenticSystemPrompt is the public Host interface method.
func (w *Worker) AssembleAgenticSystemPrompt(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	return w.assembleAgenticSystemPrompt(ctx, task, project, profile)
}

// AssembleAgenticSystemPromptWithUserContent is the public Host interface method.
func (w *Worker) AssembleAgenticSystemPromptWithUserContent(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	userContent string,
) []gateway.PromptMessage {
	return w.assembleAgenticSystemPromptWithUserContent(ctx, task, project, profile, userContent)
}

// PrependReviewRejectionFeedback is the public Host interface method.
func (w *Worker) PrependReviewRejectionFeedback(ctx context.Context, task models.Task, messages []gateway.PromptMessage) ([]gateway.PromptMessage, string) {
	return w.prependReviewRejectionFeedback(ctx, task, messages)
}

// RecordTurnSnapshot is the public Host interface method.
func (w *Worker) RecordTurnSnapshot(
	sessionID, projectID, provider, turnID string,
	messageCount, tokenCount int,
	activeTools []string,
	goalProgress float64,
) {
	w.recordTurnSnapshot(sessionID, projectID, provider, turnID, messageCount, tokenCount, activeTools, goalProgress)
}

// RecordLoopResult is the public Host interface method.
func (w *Worker) RecordLoopResult(result agentruntime.LoopResult) {
	w.recordLoopResult(result)
}

// HandleGoalStalled is the public Host interface method.
func (w *Worker) HandleGoalStalled(ctx context.Context, task models.Task, gt *agentcontext.GoalTracker) error {
	return w.handleGoalStalled(ctx, task, gt)
}