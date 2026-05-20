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
) (abort bool, result LoopResult, report bool) {
	for _, call := range resp.ToolCalls {
		taskUpdatedAt := task.UpdatedAt
		if fresh, err := w.store.GetTask(ctx, task.ID); err != nil {
			slog.Warn("failed to refresh task version for tool dispatch", "task_id", task.ID, "error", err)
		} else {
			taskUpdatedAt = fresh.UpdatedAt
		}
		tr, suspended := w.dispatchToolWithHooks(ctx, task.ID, task.ProjectID, turnID, taskUpdatedAt, call, toolToAdapter, toolExecutor, taskHooks, taskCaps)
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

func (w *Worker) finishAgenticTurnNoTools(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	content string, workPlan *Plan, goalTracker *GoalTracker,
	turnID string, turnIndex int, budgetGuard *BudgetGuard, ctxBudgetGuard *ContextBudgetGuard,
	cm *ContextManager, messages *[]gateway.PromptMessage, respecAttempts *int,
) (continueLoop bool, result LoopResult, report bool, rewindTo int, err error) {
	stalled, stallErr := w.handleGoalProgress(ctx, task, goalTracker, content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			w.handleGatewayError(ctx, task, stallErr)
		}
		return false, LoopResult{}, false, -1, stallErr
	}
	if workPlan != nil && w.planningCfg.ComplexityThreshold > 0 {
		content = w.repairOutputWithPlan(ctx, task, workPlan, content, budgetGuard)
		failing := ValidateOutput(content, *workPlan)
		if len(failing) > 0 && respecAttempts != nil && *respecAttempts < 1 && w.messageEditor != nil {
			popLastAssistantMessage(messages)
			newContent, respecErr := w.generateRespecifiedUserTurn(
				ctx, task, workPlan, failing, *messages, cm, budgetGuard,
			)
			if respecErr == nil {
				_, editErr := w.messageEditor.Edit(
					ctx, task.ID, turnID, messages, EditAnchorUserTurn, newContent,
				)
				if editErr == nil {
					*respecAttempts++
					// In-session structural repair: rewind and re-run without committing broken output.
					return true, LoopResult{}, false, 0, nil
				}
			}
		}
		content = preparePlanCommitContent(content, *workPlan)
	}
	w.commitTextWithProfile(ctx, task, content, &profile)
	r := LoopResult{
		Status: LoopSuccessfulCompletion,
		Meta: w.buildLoopMeta(
			turnIndex, budgetGuard.Usage(), totalChars(*messages), ctxBudgetGuard.TotalBudget(),
			"", "", "",
		),
	}
	return false, r, true, -1, nil
}

func popLastAssistantMessage(messages *[]gateway.PromptMessage) {
	if messages == nil || len(*messages) == 0 {
		return
	}
	last := len(*messages) - 1
	if (*messages)[last].Role == "assistant" {
		*messages = (*messages)[:last]
	}
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
	return w.applyTuning(req, task, profile)
}

func appendAssistantMessage(messages *[]gateway.PromptMessage, resp gateway.AIResponse) {
	*messages = append(*messages, gateway.PromptMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
	})
}
