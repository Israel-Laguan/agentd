package agentic

import (
	"context"
	"fmt"

	"agentd/internal/api/correlation"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// Process runs the inner agentic loop for a single task attempt.
//
// Returns (loop, result, true) when the loop stopped with a typed LoopResult
// variant. Returns (_, result, false) when the run ended with a terminal result,
// or was suspended for a human; the worker applies result to the board.
//
// The worker has already checked tool support, mounted taskHooks and taskCaps,
// run session start, and run pre-task elicitation, so task is the clarified task.
func (e *Engine) Process(
	ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile,
	taskHooks *agenthooks.HookChain, taskCaps *capabilities.Registry,
) (agentruntime.LoopResult, Result, bool) {
	cancelCtx, cleanup := e.setupAgenticCancel(ctx, task.ID)
	defer cleanup()

	// prepareAgenticRun builds messages and tools for the clarified task.
	baseProfile := profile
	messages, tools, toolToAdapter, _, _, profile, taskToolExecutor :=
		e.prepareAgenticRun(ctx, task, project, baseProfile, taskCaps)

	if !gateway.ProviderSupportsChatTools(e.config.Gateway, profile.Provider) {
		e.rejectWithoutToolSupport(cancelCtx, task, profile.Provider)
		return agentruntime.LoopResult{}, e.result, false
	}

	guards := e.newAgenticLoopGuards(cancelCtx, task)
	checkpointer := wsession.NewSessionCheckpointer(task.ID)
	messages, workPlan, planned := e.injectWorkPlanIfNeeded(cancelCtx, task, project, messages, guards.budget, checkpointer)
	if !planned {
		return agentruntime.LoopResult{}, e.result, false
	}
	sessionMgr := NewSessionManager(task.ID, extractAnchorUserContent(messages), e.config.CheckpointStore)

	loop, ok := e.runAgenticTurnLoop(agenticTurnLoopInput{
		ctx: cancelCtx, task: task, project: project, profile: profile, messages: &messages,
		tools: tools, toolToAdapter: toolToAdapter, taskToolExecutor: taskToolExecutor,
		iterationGuard: guards.iteration, budgetGuard: guards.budget, deadlineGuard: guards.deadline,
		ctxBudgetGuard: guards.ctxBudget, cm: guards.cm, goalTracker: guards.goals,
		taskHooks: taskHooks, taskCaps: taskCaps, toolTracker: guards.toolFails, workPlan: workPlan,
		sessionMgr: sessionMgr, checkpointer: checkpointer,
	})
	return loop, e.result, ok
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
	// ApplyTuning can grow the request after the iteration pre-check (for
	// example, a compressing retry appends a note). Reserve its space by
	// cutting working messages, keeping the anchor, corrections, and summary.
	if e.config.CharacterBudget > 0 {
		before := agentcontext.TotalChars(req.Messages)
		req.Messages = agentcontext.EnforceBudget(req.Messages, e.config.CharacterBudget, e.config.ContextTruncation)
		noteCut(ctx, "strategy_budget", before, agentcontext.TotalChars(req.Messages))
	}
	// SkipTruncation leaves the gateway's own cut off, so enforce the message
	// count (and any budget left after fixed context) here before generation.
	before := agentcontext.TotalChars(req.Messages)
	truncated, err := e.applyAgenticTruncation(ctx, req.Messages)
	noteCut(ctx, "agentic_head", before, agentcontext.TotalChars(truncated))
	if err != nil {
		e.result = Result{Kind: ResultGatewayError, Task: task, Err: err}
		return gateway.AIResponse{}, nil, err
	}
	req.Messages = truncated
	req.TruncatedBy = cutsFrom(ctx)
	correlation.Logger(ctx).DebugContext(ctx, "agentic: llm call start",
		"task_id", task.ID, "turn_index", turnIndex,
		"provider", req.Provider, "model", req.Model,
		"message_count", len(req.Messages), "tools_count", len(req.Tools),
	)
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
		e.result = Result{Kind: ResultGatewayError, Task: task, Err: err}
		return gateway.AIResponse{}, nil, err
	}
	e.host.RecordProviderSuccess(ctx, task)
	return resp, nil, nil
}

// rejectWithoutToolSupport fails a task whose routed provider cannot call tools.
func (e *Engine) rejectWithoutToolSupport(ctx context.Context, task models.Task, provider string) {
	correlation.Logger(ctx).Error("agentic profile's routed provider cannot call tools; failing the task",
		"task_id", task.ID, "provider", provider)
	e.result = Result{Kind: ResultFailed, Task: task, Err: fmt.Errorf("%w (%q)", models.ErrAgentProfileNoToolSupport, provider)}
}
