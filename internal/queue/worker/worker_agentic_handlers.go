package worker

import (
	"context"
	"log/slog"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func (w *Worker) prepareAgenticIteration(
	ctx context.Context, messages *[]gateway.PromptMessage,
	iterationGuard *IterationGuard, cm *ContextManager,
	ctxBudgetGuard *ContextBudgetGuard, task models.Task,
) (contextExhausted bool, err error) {
	const commentPollInterval = 5 * time.Second
	if cm.ShouldPollComments(commentPollInterval) {
		w.ingestHumanCorrections(ctx, task.ID, cm)
	}

	chars := totalChars(*messages)
	warn, preExhausted := ctxBudgetGuard.Check(chars)
	if preExhausted {
		return true, nil
	}

	var prepared []gateway.PromptMessage
	if warn {
		prepared, err = cm.PrepareContextForceSummarize(ctx, *messages)
	} else {
		prepared, err = cm.PrepareContext(ctx, *messages)
	}
	if err != nil {
		return false, err
	}
	prepared, err = w.applyAgenticTruncation(ctx, prepared)
	if err != nil {
		return false, err
	}
	*messages = prepared

	if _, exhausted := ctxBudgetGuard.Check(totalChars(*messages)); exhausted {
		return true, nil
	}

	if iterationGuard.ShouldInjectFinalMessage() {
		*messages = append(*messages, iterationGuard.FinalMessage())
		iterationGuard.ResetAllowFinal()
	}
	return false, nil
}

func (w *Worker) applyAgenticTruncation(ctx context.Context, messages []gateway.PromptMessage) ([]gateway.PromptMessage, error) {
	trunc := gateway.NewAgenticTruncator(w.truncatorMax)
	return trunc.Apply(ctx, messages, w.characterBudget)
}

func (w *Worker) handleAgenticToolCalls(
	ctx context.Context, task models.Task, turnID string,
	resp gateway.AIResponse, messages *[]gateway.PromptMessage,
	toolToAdapter map[string]string, toolExecutor *ToolExecutor,
	taskHooks *HookChain, taskCaps *capabilities.Registry,
	cm *ContextManager, toolTracker *toolFailureTracker,
	turnIndex int, budgetGuard *BudgetGuard,
	provider ...string,
) (abort bool, result LoopResult, report bool) {
	providerName := ""
	if len(provider) > 0 {
		providerName = provider[0]
	}
	for _, call := range resp.ToolCalls {
		taskUpdatedAt := task.UpdatedAt
		if fresh, err := w.store.GetTask(ctx, task.ID); err != nil {
			slog.Warn("failed to refresh task version for tool dispatch", "task_id", task.ID, "error", err)
		} else if fresh != nil {
			taskUpdatedAt = fresh.UpdatedAt
		}
		tr, suspended := w.dispatchToolWithHooks(ctx, task.ID, task.ProjectID, turnID, taskUpdatedAt, call, toolToAdapter, toolExecutor, taskHooks, taskCaps, providerName)
		contextContent := tr.ForContext()
		if detected := cm.CheckToolResult(contextContent); len(detected) > 0 {
			slog.Info("auto-detected context corrections", "task_id", task.ID, "count", len(detected))
		}
		*messages = append(*messages, gateway.PromptMessage{Role: "tool", ToolCallID: call.ID, Content: contextContent})

		if failed, _ := toolTracker.Record(call.Function.Name, tr.Status); failed || tr.Status == ToolStatusFatal {
			return true, LoopResult{
				Status: LoopToolFailure,
				Meta: w.buildLoopMeta(
					turnIndex, budgetGuard.Usage(), totalChars(*messages), 0,
					contextContent, call.Function.Name, "",
				),
			}, true
		}
		if suspended {
			return true, LoopResult{}, false
		}
	}
	return false, LoopResult{}, false
}

func (w *Worker) continueAgenticAfterTools(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	resp gateway.AIResponse, messages *[]gateway.PromptMessage,
	toolToAdapter map[string]string, toolExecutor *ToolExecutor,
	taskHooks *HookChain, taskCaps *capabilities.Registry,
	cm *ContextManager, goalTracker *GoalTracker, toolTracker *toolFailureTracker,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard,
	turnID string, turnIndex int,
) (continueLoop bool, result LoopResult, report bool, err error) {
	iterationGuard.AfterIteration(true)
	if abort, toolResult, toolReport := w.handleAgenticToolCalls(
		ctx, task, turnID, resp, messages, toolToAdapter, toolExecutor, taskHooks, taskCaps,
		cm, toolTracker, turnIndex, budgetGuard, profile.Provider,
	); abort {
		return false, toolResult, toolReport, nil
	}
	stalled, stallErr := w.handleGoalProgress(ctx, task, goalTracker, resp.Content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			w.handleGatewayError(ctx, task, stallErr)
		}
		return false, LoopResult{}, false, stallErr
	}
	return true, LoopResult{}, false, nil
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

func (w *Worker) buildAgenticRequest(
	task models.Task, profile models.AgentProfile,
	messages []gateway.PromptMessage, tools []gateway.ToolDefinition,
	sessionRecoveryGen int,
) gateway.AIRequest {
	req := gateway.AIRequest{
		Messages:       messages,
		Temperature:    profile.Temperature,
		Tools:          tools,
		AgentID:        task.AgentID,
		Role:           gateway.RoleWorker,
		TaskID:         task.ID,
		Provider:       profile.Provider,
		Model:          profile.Model,
		MaxTokens:      profile.MaxTokens,
		SkipTruncation: true,
	}
	return w.applyTuning(req, task, profile, sessionRecoveryGen)
}

func appendAssistantMessage(messages *[]gateway.PromptMessage, resp gateway.AIResponse) {
	*messages = append(*messages, gateway.PromptMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
	})
}
