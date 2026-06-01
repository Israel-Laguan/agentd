package agentic

import (
	"context"
	"log/slog"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// Process runs the inner agentic loop for a single task attempt.
//
// Returns (result, true) when the loop stopped with a typed LoopResult variant.
// Returns (_, false) when exit was handled via handoff/suspend or failHard paths.
func (e *Engine) Process(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (agentruntime.LoopResult, bool) {
	cancelCtx, cleanup := e.setupAgenticCancel(ctx, task.ID)
	defer cleanup()

	// prepareAgenticRun builds messages/tools before routing; session start and
	// pre-task elicitation run after the fallback check so legacy path is unaffected.
	messages, tools, toolToAdapter, _, profile, taskToolExecutor, taskHooks, taskCaps :=
		e.prepareAgenticRun(ctx, task, project, profile)
	if result, ok, err := e.host.TryExternalCapabilityRoute(cancelCtx, task, project, profile, &messages); err != nil {
		return agentruntime.LoopResult{}, false
	} else if ok {
		return result, true
	}
	if !gateway.ProviderSupportsChatTools(e.config.Gateway, profile.Provider) {
		slog.Warn("agentic mode requested but routed provider does not support tool round-tripping; falling back to legacy mode",
			"task_id", task.ID,
			"provider", profile.Provider,
		)
		e.host.RunLegacyTask(cancelCtx, task, project, profile, true)
		return agentruntime.LoopResult{}, false
	}

	if err := e.host.RunSessionStart(cancelCtx, task, project, taskHooks); err != nil {
		e.host.FailHard(cancelCtx, task, err)
		return agentruntime.LoopResult{}, false
	}

	task, blocked, err := e.host.RunPreTaskElicitation(cancelCtx, task, project)
	if err != nil {
		e.host.FailHard(cancelCtx, task, err)
		return agentruntime.LoopResult{}, false
	}
	if blocked {
		return agentruntime.LoopResult{}, false
	}

	guards := e.newAgenticLoopGuards(cancelCtx, task)

	checkpointer := wsession.NewSessionCheckpointer(task.ID)
	messages, workPlan := e.injectWorkPlanIfNeeded(cancelCtx, task, project, messages, guards.budget, checkpointer)

	sessionMgr := NewSessionManager(task.ID, extractAnchorUserContent(messages), e.config.CheckpointStore)

	return e.runAgenticTurnLoop(agenticTurnLoopInput{
		ctx: cancelCtx, task: task, project: project, profile: profile, messages: &messages,
		tools: tools, toolToAdapter: toolToAdapter, taskToolExecutor: taskToolExecutor,
		iterationGuard: guards.iteration, budgetGuard: guards.budget, deadlineGuard: guards.deadline,
		ctxBudgetGuard: guards.ctxBudget, cm: guards.cm, goalTracker: guards.goals,
		taskHooks: taskHooks, taskCaps: taskCaps, toolTracker: guards.toolFails, workPlan: workPlan,
		sessionMgr: sessionMgr, checkpointer: checkpointer,
	})
}

// runAgenticTurnLoop drives the inner agentic turn loop until completion, stagnation, or error.
func (e *Engine) runAgenticTurnLoop(in agenticTurnLoopInput) (agentruntime.LoopResult, bool) {
	runner := agentruntime.NewTurnLoopRunner()
	result, err := runner.Run(in.ctx, agentruntime.Request{
		TaskID: in.task.ID,
		Iterate: func(ctx context.Context, turnID string, turnIndex int, state *agentruntime.IterationState) (agentruntime.IterationOutcome, error) {
			cont, result, report, rewindTo, err := e.processAgenticIteration(
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
			resetAgenticStateForTopicDrift(&in, e)
		},
		ResetForRewind: func() {
			resetAgenticStateForRewind(in)
		},
		ApplySessionRecoveryPlanInject: func(state *agentruntime.IterationState) {
			in.sessionRecoveryNeedsPlanInject = state.SessionRecoveryNeedsPlanInject
			e.applySessionRecoveryPlanReinjection(&in)
			state.SessionRecoveryNeedsPlanInject = in.sessionRecoveryNeedsPlanInject
		},
		RecordResult: e.host.RecordLoopResult,
		BuildRewindStagnationResult: func(turnIndex int) agentruntime.LoopResult {
			return agentruntime.LoopResult{
				Status: agentruntime.LoopTurnLimitExceeded,
				Meta: agentruntime.BuildLoopMeta(
					turnIndex, in.budgetGuard.Usage(), agentcontext.TotalChars(*in.messages), in.ctxBudgetGuard.TotalBudget(),
					errRewindStagnation.Error(), "", "",
				),
			}
		},
	})
	if err != nil {
		return agentruntime.LoopResult{}, false
	}
	return result.LoopResult, result.Reported
}

func (e *Engine) generateAgenticTurn(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	budgetGuard *agentruntime.BudgetGuard, ctxBudgetGuard *agentruntime.ContextBudgetGuard, turnIndex int,
	sessionRecoveryGen int,
) (gateway.AIResponse, *agentruntime.LoopResult, error) {
	req := e.buildAgenticRequest(task, profile, *messages, tools, sessionRecoveryGen)
	resp, err := e.config.Gateway.Generate(ctx, req)
	if err != nil {
		if budgetGuard.IsBudgetExceeded(err) {
			r := agentruntime.LoopResult{
				Status: agentruntime.LoopBudgetExhausted,
				Meta: agentruntime.BuildLoopMeta(
					turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(*messages), ctxBudgetGuard.TotalBudget(),
					err.Error(), "", "token",
				),
			}
			return gateway.AIResponse{}, &r, nil
		}
		e.host.HandleGatewayError(ctx, task, err)
		return gateway.AIResponse{}, nil, err
	}
	return resp, nil, nil
}
