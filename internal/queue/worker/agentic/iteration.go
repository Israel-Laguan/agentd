package agentic

import (
	"context"
	"errors"
	"log/slog"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

var (
	errIterationLimit = errors.New("iteration limit exceeded")
	errContextBudget  = errors.New("context budget exhausted")
)

func (e *Engine) processAgenticIteration(
	ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	toolToAdapter map[string]string, toolExecutor *agenttools.ToolExecutor,
	iterationGuard *agentruntime.IterationGuard, budgetGuard *agentruntime.BudgetGuard,
	deadlineGuard *agentruntime.DeadlineGuard, ctxBudgetGuard *agentruntime.ContextBudgetGuard,
	cm *agentcontext.ContextManager, goalTracker *agentcontext.GoalTracker, sessionMgr *SessionManager,
	taskHooks *agenthooks.HookChain, taskCaps *capabilities.Registry,
	toolTracker *agenttools.ToolFailureTracker, workPlan *agentcontext.Plan, turnID string, turnIndex int,
	respecAttempts *int,
	checkpointer *wsession.SessionCheckpointer, sessionRecoveryGen *int, sessionRecoveryUsed *bool,
	sessionRecoveryNeedsPlanInject *bool,
) (continueLoop bool, result agentruntime.LoopResult, report bool, rewindTo int, err error) {
	if stop, guardErr := e.guardAgenticIteration(
		ctx, task, project, profile, messages, tools, iterationGuard, budgetGuard, deadlineGuard,
		ctxBudgetGuard, cm, goalTracker, sessionMgr, turnID, turnIndex,
	); stop != nil {
		return false, *stop, true, rewindNone, nil
	} else if guardErr != nil {
		if errors.Is(guardErr, errTopicDriftReset) {
			return true, agentruntime.LoopResult{}, false, rewindToFirstTurn, errTopicDriftReset
		}
		return false, agentruntime.LoopResult{}, false, rewindNone, guardErr
	}

	recoveryGen := 0
	if sessionRecoveryGen != nil {
		recoveryGen = *sessionRecoveryGen
	}
	resp, stop, err := e.generateAgenticTurn(ctx, task, profile, messages, tools, budgetGuard, ctxBudgetGuard, turnIndex, recoveryGen)
	if stop != nil {
		return false, *stop, true, rewindNone, nil
	}
	if err != nil {
		return false, agentruntime.LoopResult{}, false, rewindNone, err
	}
	budgetGuard.AfterCall(resp.TokenUsage)
	e.host.RecordTaskTokenUsage(ctx, task, resp.TokenUsage, resp.UsageDetails)
	if e.config.MessageEditor != nil {
		e.config.MessageEditor.CommitAssistant(messages, resp)
	} else {
		appendAssistantMessage(messages, resp)
	}

	if len(resp.ToolCalls) == 0 {
		cont, res, rep, rewind, finErr := e.finishAgenticTurnNoTools(
			ctx, task, profile, resp.Content, workPlan, goalTracker,
			turnID, turnIndex, budgetGuard, ctxBudgetGuard, cm, messages, respecAttempts,
			checkpointer, sessionRecoveryGen, sessionRecoveryUsed, sessionRecoveryNeedsPlanInject,
		)
		return cont, res, rep, rewind, finErr
	}

	cont, res, rep, finErr := e.continueAgenticAfterTools(
		ctx, task, profile, resp, messages, toolToAdapter, toolExecutor,
		taskHooks, taskCaps, cm, goalTracker, toolTracker,
		iterationGuard, budgetGuard, turnID, turnIndex,
	)
	return cont, res, rep, rewindNone, finErr
}

func (e *Engine) guardAgenticIteration(
	ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	iterationGuard *agentruntime.IterationGuard, budgetGuard *agentruntime.BudgetGuard, deadlineGuard *agentruntime.DeadlineGuard,
	ctxBudgetGuard *agentruntime.ContextBudgetGuard, cm *agentcontext.ContextManager, goalTracker *agentcontext.GoalTracker,
	sessionMgr *SessionManager, turnID string, turnIndex int,
) (*agentruntime.LoopResult, error) {
	if err := deadlineGuard.BeforeIteration(); err != nil {
		e.host.HandleGatewayError(ctx, task, err)
		return nil, err
	}
	if stop, err := e.guardIterationAndBudget(
		ctx, task, project, profile, messages, iterationGuard, budgetGuard, ctxBudgetGuard, cm, sessionMgr, turnIndex,
	); stop != nil || err != nil {
		return stop, err
	}
	e.recordAgenticTurnSnapshot(task.ID, project.ID, profile.Provider, turnID, messages, tools, budgetGuard, goalTracker)
	if err := budgetGuard.BeforeCall(); err != nil {
		return e.guardBudgetBeforeCall(ctx, task, messages, budgetGuard, ctxBudgetGuard, turnIndex, err)
	}
	return nil, nil
}

func (e *Engine) guardIterationAndBudget(
	ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
	iterationGuard *agentruntime.IterationGuard, budgetGuard *agentruntime.BudgetGuard,
	ctxBudgetGuard *agentruntime.ContextBudgetGuard, cm *agentcontext.ContextManager, sessionMgr *SessionManager,
	turnIndex int,
) (*agentruntime.LoopResult, error) {
	if err := iterationGuard.BeforeIteration(); err != nil {
		r := agentruntime.LoopResult{
			Status: agentruntime.LoopTurnLimitExceeded,
			Meta: agentruntime.BuildLoopMeta(
				turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(*messages), ctxBudgetGuard.TotalBudget(),
				err.Error(), "", "",
			),
		}
		return &r, errIterationLimit
	}
	if err := e.guardTopicDrift(ctx, task, project, profile, turnIndex, messages, sessionMgr); err != nil {
		return nil, err
	}
	contextExhausted, err := e.prepareAgenticIteration(ctx, messages, iterationGuard, cm, ctxBudgetGuard, task)
	if err != nil {
		e.host.HandleGatewayError(ctx, task, err)
		return nil, err
	}
	if contextExhausted {
		r := agentruntime.LoopResult{
			Status: agentruntime.LoopBudgetExhausted,
			Meta: agentruntime.BuildLoopMeta(
				turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(*messages), ctxBudgetGuard.TotalBudget(),
				"context character budget exhausted", "", "context",
			),
		}
		return &r, errContextBudget
	}
	return nil, nil
}

func (e *Engine) guardTopicDrift(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	turnIndex int,
	messages *[]gateway.PromptMessage,
	sessionMgr *SessionManager,
) error {
	if turnIndex == 0 || e.config.TopicGuard == nil || sessionMgr == nil {
		return nil
	}
	newInput, ok := sessionMgr.PollNewHumanInput(ctx, e.config.Store, task.ID)
	if !ok || newInput == "" {
		return nil
	}
	drift, err := e.config.TopicGuard.DetectDrift(ctx, sessionMgr.TopicAnchor(), newInput, profile)
	if err != nil {
		return err
	}
	if !drift {
		sessionMgr.AdvanceTopic(newInput)
		return nil
	}
	if _, err := sessionMgr.ArchiveAndReset(ctx, e, task, project, profile, messages, newInput); err != nil {
		return err
	}
	return errTopicDriftReset
}

func (e *Engine) recordAgenticTurnSnapshot(
	taskID, projectID, provider, turnID string, messages *[]gateway.PromptMessage,
	tools []gateway.ToolDefinition, budgetGuard *agentruntime.BudgetGuard, goalTracker *agentcontext.GoalTracker,
) {
	goalProgress := 0.0
	if goalTracker != nil {
		if g := goalTracker.Goal(); g != nil {
			goalProgress = g.ProgressRatio()
		}
	}
	e.host.RecordTurnSnapshot(
		taskID, projectID, provider, turnID,
		len(*messages),
		budgetGuard.Usage(),
		toolNamesFromDefinitions(tools),
		goalProgress,
	)
}

func (e *Engine) guardBudgetBeforeCall(
	ctx context.Context, task models.Task, messages *[]gateway.PromptMessage,
	budgetGuard *agentruntime.BudgetGuard, ctxBudgetGuard *agentruntime.ContextBudgetGuard, turnIndex int, err error,
) (*agentruntime.LoopResult, error) {
	if budgetGuard.IsBudgetExceeded(err) {
		r := agentruntime.LoopResult{
			Status: agentruntime.LoopBudgetExhausted,
			Meta: agentruntime.BuildLoopMeta(
				turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(*messages), ctxBudgetGuard.TotalBudget(),
				err.Error(), "", "token",
			),
		}
		return &r, err
	}
	e.host.HandleGatewayError(ctx, task, err)
	return nil, err
}

func (e *Engine) injectWorkPlanIfNeeded(
	ctx context.Context, task models.Task, project models.Project,
	messages []gateway.PromptMessage, budgetGuard *agentruntime.BudgetGuard,
	checkpointer *wsession.SessionCheckpointer,
) ([]gateway.PromptMessage, *agentcontext.Plan) {
	if !e.host.ShouldPlanWithBudget(task, budgetGuard) {
		return messages, nil
	}
	if checkpointer != nil {
		if err := checkpointer.Checkpoint(wsession.PrePlanCheckpointLabel, messages); err != nil {
			slog.Warn("failed to checkpoint pre_plan", "task_id", task.ID, "error", err)
		}
	}
	workPlan, planErr := e.host.GeneratePlan(ctx, task, project, budgetGuard)
	if planErr != nil {
		slog.Warn("failed to generate work plan; continuing without plan", "task_id", task.ID, "error", planErr)
		return messages, nil
	}
	if workPlan != nil {
		messages = e.host.InjectPlan(messages, workPlan)
	}
	return messages, workPlan
}

func toolNamesFromDefinitions(tools []gateway.ToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}
