package worker

import (
	"context"
	"errors"
	"log/slog"

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

	taskToolExecutor := w.newAgenticTaskToolExecutor(project, task)
	taskHooks, taskCaps := w.mountAgenticHooks(project, profile)

	messages := w.assembleAgenticSystemPrompt(ctx, task, project, profile)
	messages, _ = w.prependReviewRejectionFeedback(ctx, task, messages)
	tools, toolToAdapter := w.agenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)
	profile = w.applyModelRouting(task, profile, messages, tools)
	if !w.providerSupportsAgentic(profile) {
		slog.Warn("agentic mode requested but routed provider does not support tool round-tripping; falling back to legacy mode",
			"task_id", task.ID,
			"provider", profile.Provider,
		)
		w.runLegacyTask(cancelCtx, task, project, profile, true)
		return LoopResult{}, false
	}

	if err := w.runSessionStart(cancelCtx, task, project); err != nil {
		w.failHard(cancelCtx, task, err)
		return LoopResult{}, false
	}

	task, blocked, err := w.runPreTaskElicitation(cancelCtx, task, project)
	if err != nil {
		w.failHard(cancelCtx, task, err)
		return LoopResult{}, false
	}
	if blocked {
		return LoopResult{}, false
	}

	iterationGuard := NewIterationGuard(w.maxToolIterations)
	budgetGuard := NewBudgetGuard(w.budgetTracker, task.ID)
	deadlineGuard := NewDeadlineGuard(cancelCtx)
	cm, goalTracker := w.newAgenticContextManager(task)
	contextBudget := cm.cfg.AnchorBudget + cm.cfg.WorkingBudget + cm.cfg.CompressedBudget
	ctxBudgetGuard := NewContextBudgetGuard(contextBudget, w.contextWarningThreshold)
	toolTracker := newToolFailureTracker(w.toolFailureStreak)

	checkpointer := NewSessionCheckpointer(task.ID)
	messages, workPlan := w.injectWorkPlanIfNeeded(cancelCtx, task, project, messages, budgetGuard, checkpointer)

	sessionMgr := NewSessionManager(task.ID, extractAnchorUserContent(messages), w.checkpointStore)

	return w.runAgenticTurnLoop(agenticTurnLoopInput{
		ctx: cancelCtx, task: task, project: project, profile: profile, messages: &messages,
		tools: tools, toolToAdapter: toolToAdapter, taskToolExecutor: taskToolExecutor,
		iterationGuard: iterationGuard, budgetGuard: budgetGuard, deadlineGuard: deadlineGuard,
		ctxBudgetGuard: ctxBudgetGuard, cm: cm, goalTracker: goalTracker,
		taskHooks: taskHooks, taskCaps: taskCaps, toolTracker: toolTracker, workPlan: workPlan,
		sessionMgr: sessionMgr, checkpointer: checkpointer,
	})
}

func (w *Worker) injectWorkPlanIfNeeded(
	ctx context.Context, task models.Task, project models.Project,
	messages []gateway.PromptMessage, budgetGuard *BudgetGuard,
	checkpointer *SessionCheckpointer,
) ([]gateway.PromptMessage, *Plan) {
	if !w.shouldPlanWithBudget(task, budgetGuard) {
		return messages, nil
	}
	if checkpointer != nil {
		if err := checkpointer.Checkpoint(prePlanCheckpointLabel, messages); err != nil {
			slog.Warn("failed to checkpoint pre_plan", "task_id", task.ID, "error", err)
		}
	}
	workPlan, planErr := w.generatePlan(ctx, task, project, budgetGuard)
	if planErr != nil {
		slog.Warn("failed to generate work plan; continuing without plan", "task_id", task.ID, "error", planErr)
		return messages, nil
	}
	if workPlan != nil {
		messages = w.injectPlan(messages, workPlan)
	}
	return messages, workPlan
}

func (w *Worker) guardAgenticIteration(
	ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard, deadlineGuard *DeadlineGuard,
	ctxBudgetGuard *ContextBudgetGuard, cm *ContextManager, goalTracker *GoalTracker,
	sessionMgr *SessionManager, turnID string, turnIndex int,
) (*LoopResult, error) {
	if err := deadlineGuard.BeforeIteration(); err != nil {
		w.handleGatewayError(ctx, task, err)
		return nil, err
	}
	if stop, err := w.guardIterationAndBudget(
		ctx, task, project, profile, messages, iterationGuard, budgetGuard, ctxBudgetGuard, cm, sessionMgr, turnIndex,
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
	ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard,
	ctxBudgetGuard *ContextBudgetGuard, cm *ContextManager, sessionMgr *SessionManager,
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
	if reset, err := w.guardTopicDrift(ctx, task, project, profile, turnIndex, messages, sessionMgr); err != nil {
		return nil, err
	} else if reset {
		return nil, errTopicDriftReset
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
	ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	toolToAdapter map[string]string, toolExecutor *ToolExecutor,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard,
	deadlineGuard *DeadlineGuard, ctxBudgetGuard *ContextBudgetGuard,
	cm *ContextManager, goalTracker *GoalTracker, sessionMgr *SessionManager,
	taskHooks *HookChain, taskCaps *capabilities.Registry,
	toolTracker *toolFailureTracker, workPlan *Plan, turnID string, turnIndex int,
	respecAttempts *int,
	checkpointer *SessionCheckpointer, sessionRecoveryGen *int, sessionRecoveryUsed *bool,
	sessionRecoveryNeedsPlanInject *bool,
) (continueLoop bool, result LoopResult, report bool, rewindTo int, err error) {
	if stop, guardErr := w.guardAgenticIteration(
		ctx, task, project, profile, messages, tools, iterationGuard, budgetGuard, deadlineGuard,
		ctxBudgetGuard, cm, goalTracker, sessionMgr, turnID, turnIndex,
	); stop != nil {
		return false, *stop, true, rewindNone, nil
	} else if guardErr != nil {
		if errors.Is(guardErr, errTopicDriftReset) {
			return true, LoopResult{}, false, rewindToFirstTurn, errTopicDriftReset
		}
		return false, LoopResult{}, false, rewindNone, guardErr
	}

	recoveryGen := 0
	if sessionRecoveryGen != nil {
		recoveryGen = *sessionRecoveryGen
	}
	resp, stop, err := w.generateAgenticTurn(ctx, task, profile, messages, tools, budgetGuard, ctxBudgetGuard, turnIndex, recoveryGen)
	if stop != nil {
		return false, *stop, true, rewindNone, nil
	}
	if err != nil {
		return false, LoopResult{}, false, rewindNone, err
	}
	budgetGuard.AfterCall(resp.TokenUsage)
	if w.tokenUsageHook != nil && resp.TokenUsage > 0 {
		w.tokenUsageHook(resp.TokenUsage)
	}
	if w.messageEditor != nil {
		w.messageEditor.CommitAssistant(messages, resp)
	} else {
		appendAssistantMessage(messages, resp)
	}

	if len(resp.ToolCalls) == 0 {
		cont, res, rep, rewind, finErr := w.finishAgenticTurnNoTools(
			ctx, task, profile, resp.Content, workPlan, goalTracker,
			turnID, turnIndex, budgetGuard, ctxBudgetGuard, cm, messages, respecAttempts,
			checkpointer, sessionRecoveryGen, sessionRecoveryUsed, sessionRecoveryNeedsPlanInject,
		)
		return cont, res, rep, rewind, finErr
	}

	cont, res, rep, finErr := w.continueAgenticAfterTools(
		ctx, task, resp, messages, toolToAdapter, toolExecutor,
		taskHooks, taskCaps, cm, goalTracker, toolTracker,
		iterationGuard, budgetGuard, turnID, turnIndex,
	)
	return cont, res, rep, rewindNone, finErr
}

func (w *Worker) generateAgenticTurn(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	budgetGuard *BudgetGuard, ctxBudgetGuard *ContextBudgetGuard, turnIndex int,
	sessionRecoveryGen int,
) (gateway.AIResponse, *LoopResult, error) {
	req := w.buildAgenticRequest(task, profile, *messages, tools, sessionRecoveryGen)
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
