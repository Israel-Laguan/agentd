package worker

import (
	"context"
	"log/slog"

	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
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

	// prepareAgenticRun builds messages/tools before routing; session start and
	// pre-task elicitation run after the fallback check so legacy path is unaffected.
	messages, tools, toolToAdapter, _, profile, taskToolExecutor, taskHooks, taskCaps :=
		w.prepareAgenticRun(ctx, task, project, profile)
	if result, ok, err := w.tryExternalCapabilityRoute(cancelCtx, task, project, profile, &messages); err != nil {
		return LoopResult{}, false
	} else if ok {
		return result, true
	}
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

	guards := w.newAgenticLoopGuards(cancelCtx, task)

	checkpointer := wsession.NewSessionCheckpointer(task.ID)
	messages, workPlan := w.injectWorkPlanIfNeeded(cancelCtx, task, project, messages, guards.budget, checkpointer)

	sessionMgr := NewSessionManager(task.ID, extractAnchorUserContent(messages), w.checkpointStore)

	return w.runAgenticTurnLoop(agenticTurnLoopInput{
		ctx: cancelCtx, task: task, project: project, profile: profile, messages: &messages,
		tools: tools, toolToAdapter: toolToAdapter, taskToolExecutor: taskToolExecutor,
		iterationGuard: guards.iteration, budgetGuard: guards.budget, deadlineGuard: guards.deadline,
		ctxBudgetGuard: guards.ctxBudget, cm: guards.cm, goalTracker: guards.goals,
		taskHooks: taskHooks, taskCaps: taskCaps, toolTracker: guards.toolFails, workPlan: workPlan,
		sessionMgr: sessionMgr, checkpointer: checkpointer,
	})
}

// runAgenticTurnLoop drives the inner agentic turn loop until completion, stagnation, or error.
func (w *Worker) runAgenticTurnLoop(in agenticTurnLoopInput) (LoopResult, bool) {
	runner := agentruntime.NewTurnLoopRunner()
	result, err := runner.Run(in.ctx, agentruntime.Request{
		TaskID: in.task.ID,
		Iterate: func(ctx context.Context, turnID string, turnIndex int, state *agentruntime.IterationState) (agentruntime.IterationOutcome, error) {
			cont, result, report, rewindTo, err := w.processAgenticIteration(
				ctx, in.task, in.project, in.profile, in.messages, in.tools, in.toolToAdapter, in.taskToolExecutor,
				in.iterationGuard, in.budgetGuard, in.deadlineGuard, in.ctxBudgetGuard, in.cm, in.goalTracker, in.sessionMgr,
				in.taskHooks, in.taskCaps, in.toolTracker, in.workPlan, turnID, turnIndex, &state.RespecAttempts,
				in.checkpointer, &state.SessionRecoveryGen, &state.SessionRecoveryUsed, &state.SessionRecoveryNeedsPlanInject,
			)
			return agentruntime.IterationOutcome{
				Continue: cont,
				Result:   result,
				Report:   report,
				RewindTo: rewindTo,
			}, err
		},
		ResetForTopicDrift: func() {
			resetAgenticStateForTopicDrift(&in, w)
		},
		ResetForRewind: func() {
			resetAgenticStateForRewind(in)
		},
		ApplySessionRecoveryPlanInject: func(state *agentruntime.IterationState) {
			in.sessionRecoveryNeedsPlanInject = state.SessionRecoveryNeedsPlanInject
			w.applySessionRecoveryPlanReinjection(&in)
			state.SessionRecoveryNeedsPlanInject = in.sessionRecoveryNeedsPlanInject
		},
		RecordResult: w.recordLoopResult,
		BuildRewindStagnationResult: func(turnIndex int) LoopResult {
			return LoopResult{
				Status: LoopTurnLimitExceeded,
				Meta: w.buildLoopMeta(
					turnIndex, in.budgetGuard.Usage(), totalChars(*in.messages), in.ctxBudgetGuard.TotalBudget(),
					errRewindStagnation.Error(), "", "",
				),
			}
		},
	})
	if err != nil {
		return LoopResult{}, false
	}
	return result.LoopResult, result.Reported
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
