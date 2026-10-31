package agentic

import (
	"context"
	"log/slog"
	"time"

	"agentd/internal/api/correlation"

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
		e.ingestHumanCorrections(ctx, e.config.Store, task.ID, cm)
	}

	// The iteration-limit message goes in before the context is prepared, so
	// it counts against the budget like any other working message.
	if iterationGuard.ShouldInjectFinalMessage() {
		*messages = append(*messages, iterationGuard.FinalMessage())
		iterationGuard.ResetAllowFinal()
	}

	// Only the warning is taken from the raw size: preparing the context cuts
	// the working zone to the budget, and the check after it decides whether
	// what cannot be cut (anchor, summary, corrections) is still too large.
	warn, _ := ctxBudgetGuard.Check(agentcontext.TotalChars(*messages))

	rawChars := agentcontext.TotalChars(*messages)
	var prepared []gateway.PromptMessage
	if warn {
		prepared, err = cm.PrepareContextForceSummarize(ctx, *messages)
	} else {
		prepared, err = cm.PrepareContext(ctx, *messages)
	}
	if err != nil {
		return false, err
	}
	noteCut(ctx, "working_zone", rawChars, agentcontext.TotalChars(prepared))
	before := agentcontext.TotalChars(prepared)
	prepared, err = e.applyAgenticTruncation(ctx, prepared)
	if err != nil {
		return false, err
	}
	noteCut(ctx, "agentic_head", before, agentcontext.TotalChars(prepared))
	*messages = prepared

	_, exhausted := ctxBudgetGuard.Check(agentcontext.TotalChars(*messages))
	return exhausted, nil
}

func (e *Engine) applyAgenticTruncation(ctx context.Context, messages []gateway.PromptMessage) ([]gateway.PromptMessage, error) {
	trunc := gateway.NewAgenticTruncator(e.config.TruncatorMax)
	return trunc.Apply(ctx, messages, e.config.CharacterBudget)
}

func (e *Engine) handleAgenticToolCalls(
	ctx context.Context, task models.Task, profile models.AgentProfile, turnID string,
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
		dispatch := func(c gateway.ToolCall) (agenttools.ToolResult, bool) {
			return e.host.DispatchToolWithHooks(ctx, task.ID, task.ProjectID, turnID, taskUpdatedAt, c, toolToAdapter, toolExecutor, taskHooks, taskCaps, providerName)
		}
		tr, suspended, handedOff := e.runAgenticCall(ctx, task, profile, call, dispatch)
		if handedOff {
			return true, agentruntime.LoopResult{}, false
		}
		contextContent := tr.ForContext()
		if detected := cm.CheckToolResult(contextContent); len(detected) > 0 {
			slog.Info("auto-detected context corrections", "task_id", task.ID, "count", len(detected))
		}
		*messages = append(*messages, gateway.PromptMessage{
			Role: "tool", ToolCallID: call.ID, Content: contextContent, Section: toolResultSection(call.Function.Name),
		})

		e.noteBashOutcome(call, tr)
		e.evidence.note(call, tr)
		if stop := e.toolLoopStop(call, tr, contextContent, toolTracker, turnIndex, budgetGuard, messages); stop != nil {
			return true, *stop, true
		}
		if suspended {
			return true, agentruntime.LoopResult{}, false
		}
	}
	return false, agentruntime.LoopResult{}, false
}

// runAgenticCall runs one tool call of a turn. A call that repeats the command
// that failed first in the previous attempt is refused without running (B-043).
// Any other call runs, and one blocked at a prompt or by a permission failure
// is recovered. handedOff reports that the task went to a human.
func (e *Engine) runAgenticCall(
	ctx context.Context, task models.Task, profile models.AgentProfile, call gateway.ToolCall,
	dispatch func(gateway.ToolCall) (agenttools.ToolResult, bool),
) (tr agenttools.ToolResult, suspended, handedOff bool) {
	if feedback, repeated := e.host.RepeatedAttemptFailure(task, call); repeated {
		return agenttools.NonRetryableErrorResult(call.ID, feedback, "", 0), false, false
	}
	tr, suspended = dispatch(call)
	// ask_human runs through dispatch like any tool, so hooks can veto it; a
	// successful call blocked the task for a human, and the loop stops.
	if call.Function.Name == agenttools.ToolNameAskHuman && tr.Status == agenttools.ToolStatusSuccess {
		return tr, true, false
	}
	tr, handedOff = e.recoverBlockedCall(ctx, task, profile, call, tr, dispatch)
	if !handedOff && isBashFailure(call, tr) {
		e.host.RecordAttemptFailure(task, call, tr)
	}
	return tr, suspended, handedOff
}

// recoverBlockedCall handles a bash call that stopped at an interactive prompt
// or hit a permission failure. handedOff reports that the task went to a human,
// so the loop must stop.
func (e *Engine) recoverBlockedCall(
	ctx context.Context, task models.Task, profile models.AgentProfile, call gateway.ToolCall,
	tr agenttools.ToolResult, dispatch func(gateway.ToolCall) (agenttools.ToolResult, bool),
) (agenttools.ToolResult, bool) {
	if hang, ok := e.host.AgenticPromptHang(call, tr); ok {
		return e.recoverPromptHang(ctx, task, hang, tr)
	}
	command, blocked := e.host.AgenticPrivilegeBlock(call, tr)
	if !blocked {
		return tr, false
	}
	allowAlternative := !e.privilegeAlternativeAttempted
	e.privilegeAlternativeAttempted = true
	return e.host.RecoverAgenticPrivilege(ctx, task, profile, command, tr, allowAlternative, dispatch)
}

// recoverPromptHang answers a command that stopped at an interactive prompt.
// The first hang of a command returns corrective feedback as its tool result,
// so the model can rerun it non-interactively. The same command hanging again
// hands the task to a human.
func (e *Engine) recoverPromptHang(ctx context.Context, task models.Task, hang PromptHang, tr agenttools.ToolResult) (agenttools.ToolResult, bool) {
	e.host.Emit(ctx, task, "PROMPT_DETECTED", hang.Payload)
	if e.hungCommands[hang.Command] {
		e.settle(Result{Kind: ResultPromptHandoff, Task: task, Payload: hang.Payload})
		return tr, true
	}
	if e.hungCommands == nil {
		e.hungCommands = map[string]bool{}
	}
	e.hungCommands[hang.Command] = true
	return agenttools.NonRetryableErrorResult(tr.CallID, hang.Feedback, "", tr.ElapsedMs), false
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
	abort, toolResult, toolReport := e.handleAgenticToolCalls(
		ctx, task, profile, turnID, resp, messages, toolToAdapter, toolExecutor, taskHooks, taskCaps,
		cm, toolTracker, turnIndex, budgetGuard, resp.ProviderUsed,
	)
	correlation.Logger(ctx).DebugContext(ctx, "agentic: tool dispatch end",
		"task_id", task.ID, "turn_index", turnIndex, "turn_id", turnID,
		"tool_calls", len(resp.ToolCalls), "aborted", abort,
	)
	if abort {
		return false, toolResult, toolReport, nil
	}
	stalled, stallErr := e.handleGoalProgress(ctx, task, goalTracker, resp.Content)
	if stalled || stallErr != nil {
		if stallErr != nil {
			e.settle(Result{Kind: ResultGatewayError, Task: task, Err: stallErr})
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
		Purpose:        gateway.PurposeExecTurn,
		Messages:       messages,
		Temperature:    profile.Temperature,
		Tools:          tools,
		AgentID:        task.AgentID,
		TaskID:         task.ID,
		Attrs:          gateway.AttrsForTask(task.RequiresWorkspaceChange),
		Provider:       profile.Provider,
		Model:          profile.Model,
		MaxTokens:      profile.MaxTokens,
		SkipTruncation: true,
	}
	return e.host.ApplyTuning(req, task, profile, sessionRecoveryGen)
}

func appendAssistantMessage(messages *[]gateway.PromptMessage, resp gateway.AIResponse) {
	*messages = append(*messages, gateway.PromptMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
	})
}

// toolResultSection files a `read` result as file context (the file pipeline
// feeds it) and every other tool's output as tool results.
func toolResultSection(tool string) gateway.Section {
	if tool == "read" {
		return gateway.SectionFileContext
	}
	return gateway.SectionToolResults
}
