package worker

import (
	"context"
	"errors"
	"fmt"

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

	taskToolExecutor := w.newAgenticTaskToolExecutor(project, task)
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
	if stop, err := w.guardIterationAndBudget(
		ctx, task, messages, iterationGuard, budgetGuard, ctxBudgetGuard, cm, turnIndex,
	); stop != nil || err != nil {
		return stop, err
	}
	w.recordAgenticTurnSnapshot(task.ID, turnID, messages, tools, budgetGuard, goalTracker)
	if err := budgetGuard.BeforeCall(); err != nil {
		return w.guardBudgetBeforeCall(ctx, task, messages, budgetGuard, ctxBudgetGuard, turnIndex, err)
	}
	return nil, nil
}

func (w *Worker) guardIterationAndBudget(
	ctx context.Context, task models.Task,
	messages *[]gateway.PromptMessage,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard,
	ctxBudgetGuard *ContextBudgetGuard, cm *ContextManager,
	turnIndex int,
) (*LoopResult, error) {
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
	return nil, nil
}

func (w *Worker) recordAgenticTurnSnapshot(
	taskID, turnID string, messages *[]gateway.PromptMessage,
	tools []gateway.ToolDefinition, budgetGuard *BudgetGuard, goalTracker *GoalTracker,
) {
	goalProgress := 0.0
	if goalTracker != nil {
		if g := goalTracker.Goal(); g != nil {
			goalProgress = g.ProgressRatio()
		}
	}
	w.recordTurnSnapshot(
		taskID, turnID,
		len(*messages),
		budgetGuard.Usage(),
		toolNamesFromDefinitions(tools),
		goalProgress,
	)
}

func (w *Worker) guardBudgetBeforeCall(
	ctx context.Context, task models.Task, messages *[]gateway.PromptMessage,
	budgetGuard *BudgetGuard, ctxBudgetGuard *ContextBudgetGuard, turnIndex int, err error,
) (*LoopResult, error) {
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

	resp, stop, err := w.generateAgenticTurn(ctx, task, profile, messages, tools, budgetGuard, ctxBudgetGuard, turnIndex)
	if stop != nil {
		return false, *stop, true, nil
	}
	if err != nil {
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

	return w.continueAgenticAfterTools(
		ctx, task, resp, messages, toolToAdapter, toolExecutor,
		taskHooks, taskCaps, cm, goalTracker, toolTracker,
		iterationGuard, budgetGuard, turnID, turnIndex,
	)
}

func (w *Worker) generateAgenticTurn(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	budgetGuard *BudgetGuard, ctxBudgetGuard *ContextBudgetGuard, turnIndex int,
) (gateway.AIResponse, *LoopResult, error) {
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
			return gateway.AIResponse{}, &r, nil
		}
		w.handleGatewayError(ctx, task, err)
		return gateway.AIResponse{}, nil, err
	}
	return resp, nil, nil
}

func (w *Worker) continueAgenticAfterTools(
	ctx context.Context, task models.Task,
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
