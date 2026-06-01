package agentic

import (
	"context"
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

const (
	rewindNone        = agentruntime.RewindNone
	rewindToFirstTurn = agentruntime.RewindToFirstTurn // distinct from rewindNone: restart from turn 0 after respec edit
	maxRewindStreak   = agentruntime.MaxRewindStreak
)

var (
	errRewindStagnation = agentruntime.ErrRewindStagnation
	errTopicDriftReset  = agentruntime.ErrTopicDriftReset
)

func resetAgenticStateForRewind(in agenticTurnLoopInput) {
	if in.toolTracker != nil {
		in.toolTracker.Reset()
	}
	if in.iterationGuard != nil {
		in.iterationGuard.Reset()
	}
	if in.goalTracker != nil {
		if g := agentcontext.GoalFromTask(in.task); g != nil {
			in.goalTracker.SetGoal(*g)
		}
	}
	if in.ctxBudgetGuard != nil {
		in.ctxBudgetGuard.Reset()
	}
}

func resetAgenticStateForTopicDrift(in *agenticTurnLoopInput, e *Engine) {
	resetAgenticStateForRewind(*in)
	in.cm = e.newAgenticContextManagerOnly(in.task)
}

type agenticTurnLoopInput struct {
	ctx                            context.Context
	task                           models.Task
	project                        models.Project
	profile                        models.AgentProfile
	messages                       *[]gateway.PromptMessage
	tools                          []gateway.ToolDefinition
	toolToAdapter                  map[string]string
	taskToolExecutor               *agenttools.ToolExecutor
	iterationGuard                 *agentruntime.IterationGuard
	budgetGuard                    *agentruntime.BudgetGuard
	deadlineGuard                  *agentruntime.DeadlineGuard
	ctxBudgetGuard                 *agentruntime.ContextBudgetGuard
	cm                             *agentcontext.ContextManager
	goalTracker                    *agentcontext.GoalTracker
	taskHooks                      *agenthooks.HookChain
	taskCaps                       *capabilities.Registry
	toolTracker                    *agenttools.ToolFailureTracker
	workPlan                       *agentcontext.Plan
	sessionMgr                     *SessionManager
	checkpointer                   *wsession.SessionCheckpointer
	sessionRecoveryUsed            bool
	sessionRecoveryNeedsPlanInject bool
}

// applySessionRecoveryPlanReinjection injects the work plan once after a session-recovery rewind.
func (e *Engine) applySessionRecoveryPlanReinjection(in *agenticTurnLoopInput) {
	if in.sessionRecoveryNeedsPlanInject && in.workPlan != nil && in.messages != nil {
		*in.messages = e.host.InjectPlan(*in.messages, in.workPlan)
		in.sessionRecoveryNeedsPlanInject = false
	}
}

func (e *Engine) tryAgenticPrePlanRecovery(
	ctx context.Context, task models.Task, turnID string, redoExhausted bool,
	checkpointer *wsession.SessionCheckpointer, messages *[]gateway.PromptMessage,
	sessionRecoveryGen *int, sessionRecoveryUsed *bool, sessionRecoveryNeedsPlanInject *bool,
) bool {
	if !redoExhausted || checkpointer == nil || sessionRecoveryGen == nil {
		return false
	}
	if sessionRecoveryUsed != nil && *sessionRecoveryUsed {
		return false
	}
	if restoreErr := checkpointer.BranchFrom(wsession.PrePlanCheckpointLabel, messages); restoreErr != nil {
		slog.Warn("agentic pre_plan restore skipped",
			"task_id", task.ID, "turn_id", turnID, "error", restoreErr)
		return false
	}
	*sessionRecoveryGen++
	if sessionRecoveryUsed != nil {
		*sessionRecoveryUsed = true
	}
	if sessionRecoveryNeedsPlanInject != nil {
		*sessionRecoveryNeedsPlanInject = true
	}
	slog.Info("agentic session restored from pre_plan checkpoint",
		"task_id", task.ID, "label", wsession.PrePlanCheckpointLabel,
		"session_recovery_gen", *sessionRecoveryGen)
	return true
}

func (e *Engine) tryAgenticRespecRewind(
	ctx context.Context, task models.Task, workPlan *agentcontext.Plan, content, turnID string,
	messages *[]gateway.PromptMessage, respecAttempts *int,
	cm *agentcontext.ContextManager, budgetGuard *agentruntime.BudgetGuard,
) bool {
	failing := agentcontext.ValidateOutput(content, *workPlan)
	if len(failing) == 0 || respecAttempts == nil || *respecAttempts >= 1 || e.config.MessageEditor == nil {
		return false
	}
	msgsForRespec := messagesWithoutLastAssistant(*messages)
	newContent, respecErr := e.host.GenerateRespecifiedUserTurn(
		ctx, task, workPlan, failing, msgsForRespec, cm, budgetGuard,
	)
	if respecErr != nil {
		slog.Warn("agentic respec repair skipped",
			"task_id", task.ID, "turn_id", turnID, "error", respecErr)
		return false
	}
	if _, editErr := e.config.MessageEditor.Edit(
		ctx, task.ID, turnID, messages, agentcontext.EditAnchorUserTurn, newContent, cm,
	); editErr != nil {
		slog.Warn("agentic respec history edit skipped",
			"task_id", task.ID, "turn_id", turnID, "error", editErr)
		return false
	}
	(*respecAttempts)++
	return true
}

func (e *Engine) applyAgenticNoToolsPlanContent(
	ctx context.Context, task models.Task, content string, workPlan *agentcontext.Plan, turnID string,
	messages *[]gateway.PromptMessage, respecAttempts *int,
	checkpointer *wsession.SessionCheckpointer, sessionRecoveryGen *int, sessionRecoveryUsed *bool,
	sessionRecoveryNeedsPlanInject *bool, budgetGuard *agentruntime.BudgetGuard, cm *agentcontext.ContextManager,
) (string, bool) {
	if workPlan == nil {
		return content, false
	}
	if e.config.PlanningCfg.ComplexityThreshold <= 0 {
		return agentcontext.PreparePlanCommitContent(content, *workPlan), false
	}
	var redoExhausted bool
	content, redoExhausted = e.host.RepairOutputWithPlan(ctx, task, workPlan, content, budgetGuard)
	if e.tryAgenticPrePlanRecovery(ctx, task, turnID, redoExhausted, checkpointer, messages, sessionRecoveryGen, sessionRecoveryUsed, sessionRecoveryNeedsPlanInject) {
		return content, true
	}
	if e.tryAgenticRespecRewind(ctx, task, workPlan, content, turnID, messages, respecAttempts, cm, budgetGuard) {
		return content, true
	}
	return agentcontext.PreparePlanCommitContent(content, *workPlan), false
}

func (e *Engine) finishAgenticTurnNoTools(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	content string, workPlan *agentcontext.Plan, goalTracker *agentcontext.GoalTracker,
	turnID string, turnIndex int, budgetGuard *agentruntime.BudgetGuard, ctxBudgetGuard *agentruntime.ContextBudgetGuard,
	cm *agentcontext.ContextManager, messages *[]gateway.PromptMessage, respecAttempts *int,
	checkpointer *wsession.SessionCheckpointer, sessionRecoveryGen *int, sessionRecoveryUsed *bool,
	sessionRecoveryNeedsPlanInject *bool,
) (continueLoop bool, result agentruntime.LoopResult, report bool, rewindTo int, err error) {
	content, rewind := e.applyAgenticNoToolsPlanContent(
		ctx, task, content, workPlan, turnID, messages, respecAttempts,
		checkpointer, sessionRecoveryGen, sessionRecoveryUsed, sessionRecoveryNeedsPlanInject, budgetGuard, cm,
	)
	if rewind {
		return true, agentruntime.LoopResult{}, false, rewindToFirstTurn, nil
	}
	stalled, stallErr := e.handleGoalProgress(ctx, task, goalTracker, content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			e.host.HandleGatewayError(ctx, task, stallErr)
		}
		return false, agentruntime.LoopResult{}, false, rewindNone, stallErr
	}
	e.host.CommitTextWithProfile(ctx, task, content, &profile)
	r := agentruntime.LoopResult{
		Status: agentruntime.LoopSuccessfulCompletion,
		Meta: agentruntime.BuildLoopMeta(
			turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(*messages), ctxBudgetGuard.TotalBudget(),
			"", "", "",
		),
	}
	return false, r, true, rewindNone, nil
}

// messagesWithoutLastAssistant returns a copy of messages omitting a trailing assistant
// message, used for respec input without mutating the live conversation slice.
func messagesWithoutLastAssistant(messages []gateway.PromptMessage) []gateway.PromptMessage {
	if len(messages) == 0 {
		return nil
	}
	last := len(messages) - 1
	if messages[last].Role != "assistant" {
		out := make([]gateway.PromptMessage, len(messages))
		copy(out, messages)
		return out
	}
	out := make([]gateway.PromptMessage, last)
	copy(out, messages[:last])
	return out
}
