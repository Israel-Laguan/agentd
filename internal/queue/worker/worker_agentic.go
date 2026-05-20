package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// processAgentic runs the inner agentic loop for a single task attempt.
//
// Returns (result, true) when the loop stopped with a typed LoopResult variant.
// Returns (_, false) when exit was handled via handoff/suspend or failHard paths.
func (w *Worker) processAgentic(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (LoopResult, bool) {
	cancelCtx, cleanup := w.setupAgenticCancel(ctx, task.ID)
	defer cleanup()

	if err := w.runSessionStart(cancelCtx, task, project); err != nil {
		w.failHard(cancelCtx, task, err)
		return LoopResult{}, false
	}

	taskToolExecutor := w.newAgenticTaskToolExecutor(project)
	taskHooks, taskCaps := w.mountAgenticHooks(project, profile)

	messages := w.assembleAgenticSystemPrompt(ctx, task, project, profile)
	messages = w.prependReviewRejectionFeedback(ctx, task, messages)
	tools, toolToAdapter := w.agenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)

	iterationGuard := NewIterationGuard(w.maxToolIterations)
	budgetGuard := NewBudgetGuard(w.budgetTracker, task.ID)
	deadlineGuard := NewDeadlineGuard(cancelCtx)
	cm, goalTracker := w.newAgenticContextManager(task)
	contextBudget := cm.cfg.AnchorBudget + cm.cfg.WorkingBudget + cm.cfg.CompressedBudget
	ctxBudgetGuard := NewContextBudgetGuard(contextBudget, w.contextWarningThreshold)
	toolTracker := newToolFailureTracker(w.toolFailureStreak)

	for turnIndex := 0; ; turnIndex++ {
		turnID := fmt.Sprintf("%s:%d", task.ID, turnIndex)
		cont, result, report, err := w.processAgenticIteration(
			cancelCtx, task, profile, &messages, tools, toolToAdapter, taskToolExecutor,
			iterationGuard, budgetGuard, deadlineGuard, ctxBudgetGuard, cm, goalTracker,
			taskHooks, taskCaps, toolTracker, turnID, turnIndex,
		)
		if err != nil {
			return LoopResult{}, false
		}
		if report {
			w.recordLoopResult(result)
			return result, true
		}
		if !cont {
			return LoopResult{}, false
		}
	}
}

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

func (w *Worker) guardAgenticIteration(
	ctx context.Context, task models.Task,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard, deadlineGuard *DeadlineGuard,
	ctxBudgetGuard *ContextBudgetGuard, cm *ContextManager, goalTracker *GoalTracker,
	turnID string, turnIndex int,
) (*LoopResult, error) {
	if err := deadlineGuard.BeforeIteration(); err != nil {
		w.handleGatewayError(ctx, task, err)
		return nil, err
	}
	if err := iterationGuard.BeforeIteration(); err != nil {
		r := LoopResult{
			Status: LoopTurnLimitExceeded,
			Meta: w.buildLoopMeta(
				turnIndex, budgetGuard.Usage(), totalChars(*messages), ctxBudgetGuard.TotalBudget(),
				err.Error(), "", "",
			),
		}
		return &r, errIterationLimit
	}
	contextExhausted, err := w.prepareAgenticIteration(ctx, messages, iterationGuard, cm, ctxBudgetGuard, task)
	if err != nil {
		w.handleGatewayError(ctx, task, err)
		return nil, err
	}
	if contextExhausted {
		r := LoopResult{
			Status: LoopBudgetExhausted,
			Meta: w.buildLoopMeta(
				turnIndex, budgetGuard.Usage(), totalChars(*messages), ctxBudgetGuard.TotalBudget(),
				"context character budget exhausted", "", "context",
			),
		}
		return &r, errContextBudget
	}
	goalProgress := 0.0
	if goalTracker != nil {
		if g := goalTracker.Goal(); g != nil {
			goalProgress = g.ProgressRatio()
		}
	}
	w.recordTurnSnapshot(
		task.ID, turnID,
		len(*messages),
		budgetGuard.Usage(),
		toolNamesFromDefinitions(tools),
		goalProgress,
	)
	if err := budgetGuard.BeforeCall(); err != nil {
		if budgetGuard.IsBudgetExceeded(err) {
			r := LoopResult{
				Status: LoopBudgetExhausted,
				Meta: w.buildLoopMeta(
					turnIndex, budgetGuard.Usage(), totalChars(*messages), ctxBudgetGuard.TotalBudget(),
					err.Error(), "", "token",
				),
			}
			return &r, err
		}
		w.handleGatewayError(ctx, task, err)
		return nil, err
	}
	return nil, nil
}

var (
	errIterationLimit = errors.New("iteration limit exceeded")
	errContextBudget  = errors.New("context budget exhausted")
)

func (w *Worker) processAgenticIteration(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	toolToAdapter map[string]string, toolExecutor *ToolExecutor,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard,
	deadlineGuard *DeadlineGuard, ctxBudgetGuard *ContextBudgetGuard,
	cm *ContextManager, goalTracker *GoalTracker,
	taskHooks *HookChain, taskCaps *capabilities.Registry,
	toolTracker *toolFailureTracker, turnID string, turnIndex int,
) (continueLoop bool, result LoopResult, report bool, err error) {
	if stop, guardErr := w.guardAgenticIteration(
		ctx, task, messages, tools, iterationGuard, budgetGuard, deadlineGuard,
		ctxBudgetGuard, cm, goalTracker, turnID, turnIndex,
	); stop != nil {
		return false, *stop, true, nil
	} else if guardErr != nil {
		return false, LoopResult{}, false, guardErr
	}

	req := w.buildAgenticRequest(task, profile, *messages, tools)
	resp, err := w.gateway.Generate(ctx, req)
	if err != nil {
		if budgetGuard.IsBudgetExceeded(err) {
			r := LoopResult{
				Status: LoopBudgetExhausted,
				Meta: w.buildLoopMeta(
					turnIndex, budgetGuard.Usage(), totalChars(*messages), ctxBudgetGuard.TotalBudget(),
					err.Error(), "", "token",
				),
			}
			return false, r, true, nil
		}
		w.handleGatewayError(ctx, task, err)
		return false, LoopResult{}, false, err
	}
	budgetGuard.AfterCall(resp.TokenUsage)
	if w.tokenUsageHook != nil && resp.TokenUsage > 0 {
		w.tokenUsageHook(resp.TokenUsage)
	}
	appendAssistantMessage(messages, resp)

	if len(resp.ToolCalls) == 0 {
		return w.finishAgenticTurnNoTools(ctx, task, profile, resp.Content, goalTracker, turnIndex, budgetGuard, ctxBudgetGuard, messages)
	}

	iterationGuard.AfterIteration(true)
	if abort, toolResult, toolReport := w.handleAgenticToolCalls(
		ctx, task, turnID, resp, messages, toolToAdapter, toolExecutor, taskHooks, taskCaps,
		cm, toolTracker, turnIndex, budgetGuard,
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

func (w *Worker) finishAgenticTurnNoTools(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	content string, goalTracker *GoalTracker,
	turnIndex int, budgetGuard *BudgetGuard, ctxBudgetGuard *ContextBudgetGuard,
	messages *[]gateway.PromptMessage,
) (continueLoop bool, result LoopResult, report bool, err error) {
	stalled, stallErr := w.handleGoalProgress(ctx, task, goalTracker, content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			w.handleGatewayError(ctx, task, stallErr)
		}
		return false, LoopResult{}, false, stallErr
	}
	w.commitTextWithProfile(ctx, task, content, &profile)
	r := LoopResult{
		Status: LoopSuccessfulCompletion,
		Meta: w.buildLoopMeta(
			turnIndex, budgetGuard.Usage(), totalChars(*messages), ctxBudgetGuard.TotalBudget(),
			"", "", "",
		),
	}
	return false, r, true, nil
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
