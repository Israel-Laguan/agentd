package worker

import (
	"context"
	"errors"
	"log/slog"

	wsession "agentd/internal/agent/session"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

const (
	rewindNone        = -1
	rewindToFirstTurn = 0 // distinct from rewindNone: restart from turn 0 after respec edit
	maxRewindStreak   = 3
)

var (
	errRewindStagnation = errors.New("rewind stagnation")
	errTopicDriftReset  = errors.New("topic drift: session reset")
)

// agenticRewindState tracks repeated rewinds to the same turn index.
type agenticRewindState struct {
	lastTarget int
	streak     int
}

// apply records a rewind request. Returns stagnation when the same target repeats
// more than maxRewindStreak times.
func (s *agenticRewindState) apply(rewindTo int) (stagnation bool) {
	if rewindTo == s.lastTarget {
		s.streak++
	} else {
		s.lastTarget = rewindTo
		s.streak = 1
	}
	return s.streak > maxRewindStreak
}

func (s *agenticRewindState) reset() {
	s.lastTarget = rewindNone
	s.streak = 0
}

func resetAgenticStateForRewind(in agenticTurnLoopInput) {
	if in.toolTracker != nil {
		in.toolTracker.Reset()
	}
	if in.iterationGuard != nil {
		in.iterationGuard.reset()
	}
	if in.goalTracker != nil {
		if g := GoalFromTask(in.task); g != nil {
			in.goalTracker.SetGoal(*g)
		}
	}
	if in.ctxBudgetGuard != nil {
		in.ctxBudgetGuard.reset()
	}
}

func resetAgenticStateForTopicDrift(in *agenticTurnLoopInput, w *Worker) {
	resetAgenticStateForRewind(*in)
	in.cm = w.newAgenticContextManagerOnly(in.task)
}

type agenticTurnLoopInput struct {
	ctx                            context.Context
	task                           models.Task
	project                        models.Project
	profile                        models.AgentProfile
	messages                       *[]gateway.PromptMessage
	tools                          []gateway.ToolDefinition
	toolToAdapter                  map[string]string
	taskToolExecutor               *ToolExecutor
	iterationGuard                 *IterationGuard
	budgetGuard                    *BudgetGuard
	deadlineGuard                  *DeadlineGuard
	ctxBudgetGuard                 *ContextBudgetGuard
	cm                             *ContextManager
	goalTracker                    *GoalTracker
	taskHooks                      *HookChain
	taskCaps                       *capabilities.Registry
	toolTracker                    *toolFailureTracker
	workPlan                       *Plan
	sessionMgr                     *SessionManager
	checkpointer                   *wsession.SessionCheckpointer
	sessionRecoveryGen             int
	sessionRecoveryUsed            bool
	sessionRecoveryNeedsPlanInject bool
}

// applySessionRecoveryPlanReinjection injects the work plan once after a session-recovery rewind.
func (w *Worker) applySessionRecoveryPlanReinjection(in *agenticTurnLoopInput) {
	if in.sessionRecoveryNeedsPlanInject && in.workPlan != nil && in.messages != nil {
		*in.messages = w.injectPlan(*in.messages, in.workPlan)
		in.sessionRecoveryNeedsPlanInject = false
	}
}

func (w *Worker) tryAgenticPrePlanRecovery(
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

func (w *Worker) tryAgenticRespecRewind(
	ctx context.Context, task models.Task, workPlan *Plan, content, turnID string,
	messages *[]gateway.PromptMessage, respecAttempts *int,
	cm *ContextManager, budgetGuard *BudgetGuard,
) bool {
	failing := ValidateOutput(content, *workPlan)
	if len(failing) == 0 || respecAttempts == nil || *respecAttempts >= 1 || w.messageEditor == nil {
		return false
	}
	msgsForRespec := messagesWithoutLastAssistant(*messages)
	newContent, respecErr := w.generateRespecifiedUserTurn(
		ctx, task, workPlan, failing, msgsForRespec, cm, budgetGuard,
	)
	if respecErr != nil {
		slog.Warn("agentic respec repair skipped",
			"task_id", task.ID, "turn_id", turnID, "error", respecErr)
		return false
	}
	if _, editErr := w.messageEditor.Edit(
		ctx, task.ID, turnID, messages, EditAnchorUserTurn, newContent, cm,
	); editErr != nil {
		slog.Warn("agentic respec history edit skipped",
			"task_id", task.ID, "turn_id", turnID, "error", editErr)
		return false
	}
	(*respecAttempts)++
	return true
}

func (w *Worker) applyAgenticNoToolsPlanContent(
	ctx context.Context, task models.Task, content string, workPlan *Plan, turnID string,
	messages *[]gateway.PromptMessage, respecAttempts *int,
	checkpointer *wsession.SessionCheckpointer, sessionRecoveryGen *int, sessionRecoveryUsed *bool,
	sessionRecoveryNeedsPlanInject *bool, budgetGuard *BudgetGuard, cm *ContextManager,
) (string, bool) {
	if workPlan == nil {
		return content, false
	}
	if w.planningCfg.ComplexityThreshold <= 0 {
		return preparePlanCommitContent(content, *workPlan), false
	}
	var redoExhausted bool
	content, redoExhausted = w.repairOutputWithPlan(ctx, task, workPlan, content, budgetGuard)
	if w.tryAgenticPrePlanRecovery(ctx, task, turnID, redoExhausted, checkpointer, messages, sessionRecoveryGen, sessionRecoveryUsed, sessionRecoveryNeedsPlanInject) {
		return content, true
	}
	if w.tryAgenticRespecRewind(ctx, task, workPlan, content, turnID, messages, respecAttempts, cm, budgetGuard) {
		return content, true
	}
	return preparePlanCommitContent(content, *workPlan), false
}

func (w *Worker) finishAgenticTurnNoTools(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	content string, workPlan *Plan, goalTracker *GoalTracker,
	turnID string, turnIndex int, budgetGuard *BudgetGuard, ctxBudgetGuard *ContextBudgetGuard,
	cm *ContextManager, messages *[]gateway.PromptMessage, respecAttempts *int,
	checkpointer *wsession.SessionCheckpointer, sessionRecoveryGen *int, sessionRecoveryUsed *bool,
	sessionRecoveryNeedsPlanInject *bool,
) (continueLoop bool, result LoopResult, report bool, rewindTo int, err error) {
	content, rewind := w.applyAgenticNoToolsPlanContent(
		ctx, task, content, workPlan, turnID, messages, respecAttempts,
		checkpointer, sessionRecoveryGen, sessionRecoveryUsed, sessionRecoveryNeedsPlanInject, budgetGuard, cm,
	)
	if rewind {
		return true, LoopResult{}, false, rewindToFirstTurn, nil
	}
	stalled, stallErr := w.handleGoalProgress(ctx, task, goalTracker, content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			w.handleGatewayError(ctx, task, stallErr)
		}
		return false, LoopResult{}, false, rewindNone, stallErr
	}
	w.commitTextWithProfile(ctx, task, content, &profile)
	r := LoopResult{
		Status: LoopSuccessfulCompletion,
		Meta: w.buildLoopMeta(
			turnIndex, budgetGuard.Usage(), totalChars(*messages), ctxBudgetGuard.TotalBudget(),
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
