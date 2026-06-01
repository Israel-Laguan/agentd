package agentic

import (
	"context"
	"log/slog"
	"strings"
	"time"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func (e *Engine) prepareAgenticIteration(
	ctx context.Context, messages *[]gateway.PromptMessage,
	iterationGuard *agentruntime.IterationGuard, cm *agentcontext.ContextManager,
	ctxBudgetGuard *agentruntime.ContextBudgetGuard, task models.Task,
) (contextExhausted bool, err error) {
	const commentPollInterval = 5 * time.Second
	if cm.ShouldPollComments(commentPollInterval) {
		e.ingestHumanCorrections(ctx, task.ID, cm)
	}

	chars := agentcontext.TotalChars(*messages)
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
	prepared, err = e.applyAgenticTruncation(ctx, prepared)
	if err != nil {
		return false, err
	}
	*messages = prepared

	if _, exhausted := ctxBudgetGuard.Check(agentcontext.TotalChars(*messages)); exhausted {
		return true, nil
	}

	if iterationGuard.ShouldInjectFinalMessage() {
		*messages = append(*messages, iterationGuard.FinalMessage())
		iterationGuard.ResetAllowFinal()
	}
	return false, nil
}

func (e *Engine) applyAgenticTruncation(ctx context.Context, messages []gateway.PromptMessage) ([]gateway.PromptMessage, error) {
	trunc := gateway.NewAgenticTruncator(e.config.TruncatorMax)
	return trunc.Apply(ctx, messages, e.config.CharacterBudget)
}

func (e *Engine) handleAgenticToolCalls(
	ctx context.Context, task models.Task, turnID string,
	resp gateway.AIResponse, messages *[]gateway.PromptMessage,
	toolToAdapter map[string]string, toolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain, taskCaps *capabilities.Registry,
	cm *agentcontext.ContextManager, toolTracker *agenttools.ToolFailureTracker,
	turnIndex int, budgetGuard *agentruntime.BudgetGuard,
	provider ...string,
) (abort bool, result agentruntime.LoopResult, report bool) {
	providerName := ""
	if len(provider) > 0 {
		providerName = provider[0]
	}
	for _, call := range resp.ToolCalls {
		taskUpdatedAt := task.UpdatedAt
		if fresh, err := e.config.Store.GetTask(ctx, task.ID); err != nil {
			slog.Warn("failed to refresh task version for tool dispatch", "task_id", task.ID, "error", err)
		} else if fresh != nil {
			taskUpdatedAt = fresh.UpdatedAt
		}
		tr, suspended := e.host.DispatchToolWithHooks(ctx, task.ID, task.ProjectID, turnID, taskUpdatedAt, call, toolToAdapter, toolExecutor, taskHooks, taskCaps, providerName)
		contextContent := tr.ForContext()
		if detected := cm.CheckToolResult(contextContent); len(detected) > 0 {
			slog.Info("auto-detected context corrections", "task_id", task.ID, "count", len(detected))
		}
		*messages = append(*messages, gateway.PromptMessage{Role: "tool", ToolCallID: call.ID, Content: contextContent})

		if failed, _ := toolTracker.Record(call.Function.Name, tr.Status); failed || tr.Status == agenttools.ToolStatusFatal {
			return true, agentruntime.LoopResult{
				Status: agentruntime.LoopToolFailure,
				Meta: agentruntime.BuildLoopMeta(
					turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(*messages), 0,
					contextContent, call.Function.Name, "",
				),
			}, true
		}
		if suspended {
			return true, agentruntime.LoopResult{}, false
		}
	}
	return false, agentruntime.LoopResult{}, false
}

func (e *Engine) continueAgenticAfterTools(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	resp gateway.AIResponse, messages *[]gateway.PromptMessage,
	toolToAdapter map[string]string, toolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain, taskCaps *capabilities.Registry,
	cm *agentcontext.ContextManager, goalTracker *agentcontext.GoalTracker, toolTracker *agenttools.ToolFailureTracker,
	iterationGuard *agentruntime.IterationGuard, budgetGuard *agentruntime.BudgetGuard,
	turnID string, turnIndex int,
) (continueLoop bool, result agentruntime.LoopResult, report bool, err error) {
	iterationGuard.AfterIteration(true)
	if abort, toolResult, toolReport := e.handleAgenticToolCalls(
		ctx, task, turnID, resp, messages, toolToAdapter, toolExecutor, taskHooks, taskCaps,
		cm, toolTracker, turnIndex, budgetGuard, resp.ProviderUsed,
	); abort {
		return false, toolResult, toolReport, nil
	}
	stalled, stallErr := e.handleGoalProgress(ctx, task, goalTracker, resp.Content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			e.host.HandleGatewayError(ctx, task, stallErr)
		}
		return false, agentruntime.LoopResult{}, false, stallErr
	}
	return true, agentruntime.LoopResult{}, false, nil
}

func (e *Engine) handleGoalProgress(ctx context.Context, task models.Task, goalTracker *agentcontext.GoalTracker, content string) (bool, error) {
	if goalTracker == nil || goalTracker.Goal() == nil {
		return false, nil
	}
	completed, blocked := agentcontext.ParseGoalProgress(content)
	if stalled := goalTracker.AfterTurn(ctx, completed, blocked); stalled {
		if err := e.host.HandleGoalStalled(ctx, task, goalTracker); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

func (e *Engine) buildAgenticRequest(
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
	return e.host.ApplyTuning(req, task, profile, sessionRecoveryGen)
}

func (e *Engine) ingestHumanCorrections(ctx context.Context, taskID string, cm *agentcontext.ContextManager) {
	if cm == nil {
		return
	}
	comments, err := e.config.Store.ListCommentsSince(ctx, taskID, cm.CommentHighWater())
	if err != nil {
		slog.Warn("failed to list task comments for corrections", "task_id", taskID, "error", err)
		return
	}
	defer cm.AdvanceCommentHighWater(comments)
	for _, c := range comments {
		source, ok := correctionSourceForCommentAuthor(c.Author)
		if !ok {
			continue
		}
		if !cm.MarkCommentCorrectionSeen(c) {
			continue
		}
		if rec := agentcontext.ParseCorrectionComment(c.Body, source); rec != nil {
			cm.InjectCorrection(*rec)
		}
	}
}

func correctionSourceForCommentAuthor(author models.CommentAuthor) (agentcontext.CorrectionSource, bool) {
	switch author {
	case models.CommentAuthorUser, models.CommentAuthorFrontdesk:
		return agentcontext.CorrectionSourceHuman, true
	default:
		if strings.EqualFold(string(author), string(agentcontext.CorrectionSourceReviewer)) {
			return agentcontext.CorrectionSourceReviewer, true
		}
		return "", false
	}
}

func appendAssistantMessage(messages *[]gateway.PromptMessage, resp gateway.AIResponse) {
	*messages = append(*messages, gateway.PromptMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
	})
}
