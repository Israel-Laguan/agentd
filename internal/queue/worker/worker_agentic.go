package worker

import (
	"context"
	"log/slog"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func (w *Worker) processAgentic(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) {
	cancelCtx, cleanup := w.setupAgenticCancel(ctx, task.ID)
	defer cleanup()

	taskToolExecutor := w.newAgenticTaskToolExecutor(project)
	taskHooks, taskCaps := w.mountAgenticHooks(project, profile)

	messages := w.assembleAgenticSystemPrompt(ctx, task, project, profile)
	messages = w.prependReviewRejectionFeedback(ctx, task, messages)
	tools, toolToAdapter := w.agenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)

	iterationGuard := NewIterationGuard(w.maxToolIterations)
	budgetGuard := NewBudgetGuard(w.budgetTracker, task.ID)
	deadlineGuard := NewDeadlineGuard(cancelCtx)
	cm, goalTracker := w.newAgenticContextManager(task)

	for {
		shouldContinue, err := w.processAgenticIteration(
			cancelCtx, task, profile, &messages, tools, toolToAdapter, taskToolExecutor,
			iterationGuard, budgetGuard, deadlineGuard, cm, goalTracker,
			taskHooks, taskCaps,
		)
		if err != nil {
			return
		}
		if !shouldContinue {
			return
		}
	}
}

func (w *Worker) setupAgenticCancel(ctx context.Context, taskID string) (context.Context, func()) {
	cancelCtx, cancel := context.WithCancel(ctx)
	w.registerCancel(taskID, cancel)
	return cancelCtx, func() {
		cancel()
		w.deregisterCancel(taskID)
	}
}

func (w *Worker) newAgenticTaskToolExecutor(project models.Project) *ToolExecutor {
	return NewToolExecutor(
		w.sandbox,
		project.WorkspacePath,
		BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv),
		w.sandboxWallTimeout,
	)
}

func (w *Worker) mountAgenticHooks(project models.Project, profile models.AgentProfile) (*HookChain, *capabilities.Registry) {
	taskHooks, taskCaps := w.mountScopedPlugins(project, profile)
	if len(profile.GatedTools) > 0 {
		if taskHooks == nil {
			taskHooks = NewHookChain()
		}
		handler := NewBlockingApprovalHandler(w.store)
		taskHooks.RegisterPre(ApprovalGateHook(profile.GatedTools, handler))
	}
	return taskHooks, taskCaps
}

func (w *Worker) newAgenticContextManager(task models.Task) (*ContextManager, *GoalTracker) {
	contextCfg := w.contextCfg
	if contextCfg.RollingThresholdTurns <= 0 {
		contextCfg.RollingThresholdTurns = config.DefaultRollingThresholdTurns
	}
	if contextCfg.KeepRecentTurns <= 0 {
		contextCfg.KeepRecentTurns = config.DefaultKeepRecentTurns
	}
	if contextCfg.AnchorBudget <= 0 {
		contextCfg.AnchorBudget = config.DefaultAnchorBudget
	}
	if contextCfg.WorkingBudget <= 0 {
		contextCfg.WorkingBudget = config.DefaultWorkingBudget
	}
	if contextCfg.CompressedBudget <= 0 {
		contextCfg.CompressedBudget = config.DefaultCompressedBudget
	}

	cm := NewContextManager(
		contextCfg,
		w.gateway,
		task.AgentID,
		task.ID,
	)

	goal := GoalFromTask(task)
	goalTracker := NewGoalTracker(task.ID, task.ProjectID)
	if goal != nil {
		goalTracker.SetGoal(*goal)
		cm.SetGoalTracker(goalTracker)
	}
	return cm, goalTracker
}

func (w *Worker) prepareAgenticIteration(
	ctx context.Context, messages *[]gateway.PromptMessage,
	iterationGuard *IterationGuard, cm *ContextManager,
	task models.Task,
) error {
	const commentPollInterval = 5 * time.Second
	if cm.ShouldPollComments(commentPollInterval) {
		w.ingestHumanCorrections(ctx, task.ID, cm)
	}
	prepared, err := cm.PrepareContext(ctx, *messages)
	if err != nil {
		return err
	}
	*messages = prepared
	if iterationGuard.ShouldInjectFinalMessage() {
		*messages = append(*messages, iterationGuard.FinalMessage())
		iterationGuard.ResetAllowFinal()
	}
	return nil
}

func (w *Worker) handleAgenticToolCalls(
	ctx context.Context, task models.Task,
	resp gateway.AIResponse, messages *[]gateway.PromptMessage,
	toolToAdapter map[string]string, toolExecutor *ToolExecutor,
	taskHooks *HookChain, taskCaps *capabilities.Registry,
	cm *ContextManager,
) bool {
	for _, call := range resp.ToolCalls {
		taskUpdatedAt := task.UpdatedAt
		if fresh, err := w.store.GetTask(ctx, task.ID); err != nil {
			slog.Warn("failed to refresh task version for tool dispatch", "task_id", task.ID, "error", err)
		} else {
			taskUpdatedAt = fresh.UpdatedAt
		}
		result, suspended := w.dispatchToolWithHooks(ctx, task.ID, task.ProjectID, taskUpdatedAt, call, toolToAdapter, toolExecutor, taskHooks, taskCaps)
		if detected := cm.CheckToolResult(result); len(detected) > 0 {
			slog.Info("auto-detected context corrections",
				"task_id", task.ID,
				"count", len(detected),
			)
		}
		*messages = append(*messages, gateway.PromptMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    result,
		})
		if suspended {
			return true
		}
	}
	return false
}

func (w *Worker) processAgenticIteration(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	toolToAdapter map[string]string, toolExecutor *ToolExecutor,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard,
	deadlineGuard *DeadlineGuard, cm *ContextManager, goalTracker *GoalTracker,
	taskHooks *HookChain, taskCaps *capabilities.Registry,
) (bool, error) {
	if err := deadlineGuard.BeforeIteration(); err != nil {
		w.handleGatewayError(ctx, task, err)
		return false, err
	}
	if err := iterationGuard.BeforeIteration(); err != nil {
		w.handleIterationExceeded(ctx, task)
		return false, err
	}
	if err := w.prepareAgenticIteration(ctx, messages, iterationGuard, cm, task); err != nil {
		w.handleGatewayError(ctx, task, err)
		return false, err
	}
	if err := budgetGuard.BeforeCall(); err != nil {
		w.handleGatewayError(ctx, task, err)
		return false, err
	}

	req := w.buildAgenticRequest(task, profile, *messages, tools)
	resp, err := w.gateway.Generate(ctx, req)
	if err != nil {
		w.handleGatewayError(ctx, task, err)
		return false, err
	}
	budgetGuard.AfterCall(resp.TokenUsage)
	appendAssistantMessage(messages, resp)

	if len(resp.ToolCalls) == 0 {
		return w.finishAgenticTurnNoTools(ctx, task, profile, resp.Content, goalTracker)
	}

	iterationGuard.AfterIteration(true)
	if w.handleAgenticToolCalls(ctx, task, resp, messages, toolToAdapter, toolExecutor, taskHooks, taskCaps, cm) {
		return false, nil
	}

	stalled, stallErr := w.handleGoalProgress(ctx, task, goalTracker, resp.Content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			w.handleGatewayError(ctx, task, stallErr)
		}
		return false, stallErr
	}

	return true, nil
}

func (w *Worker) buildAgenticRequest(
	task models.Task, profile models.AgentProfile,
	messages []gateway.PromptMessage, tools []gateway.ToolDefinition,
) gateway.AIRequest {
	req := gateway.AIRequest{
		Messages:    messages,
		Temperature: profile.Temperature,
		Tools:       tools,
		AgentID:     task.AgentID,
		Role:        gateway.RoleWorker,
		TaskID:      task.ID,
		Provider:    profile.Provider,
		Model:       profile.Model,
		MaxTokens:   profile.MaxTokens,
	}
	return w.applyTuning(req, task, profile)
}

func appendAssistantMessage(messages *[]gateway.PromptMessage, resp gateway.AIResponse) {
	*messages = append(*messages, gateway.PromptMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
	})
}

func (w *Worker) finishAgenticTurnNoTools(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	content string, goalTracker *GoalTracker,
) (bool, error) {
	stalled, stallErr := w.handleGoalProgress(ctx, task, goalTracker, content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			w.handleGatewayError(ctx, task, stallErr)
		}
		return false, stallErr
	}
	w.commitTextWithProfile(ctx, task, content, &profile)
	return false, nil
}

func (w *Worker) handleGoalProgress(ctx context.Context, task models.Task, goalTracker *GoalTracker, content string) (bool, error) {
	if goalTracker == nil || goalTracker.Goal() == nil {
		return false, nil
	}
	completed, blocked := parseGoalProgress(content)
	if stalled := goalTracker.AfterTurn(ctx, completed, blocked); stalled {
		if err := w.handleGoalStalled(ctx, task, goalTracker); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

func (w *Worker) agenticTools(ctx context.Context, toolExecutor *ToolExecutor) ([]gateway.ToolDefinition, map[string]string) {
	tools := append([]gateway.ToolDefinition(nil), toolExecutor.Definitions()...)
	tools = append(tools, DelegateToolDefinition(), DelegateParallelToolDefinition())
	if w.capabilities == nil {
		return tools, nil
	}
	capabilityTools, toolToAdapter, err := w.capabilities.GetToolsAndAdapterIndex(ctx)
	if err != nil {
		slog.Warn("failed to get capability tools", "error", err)
		return tools, nil
	}
	return append(tools, capabilityTools...), toolToAdapter
}
