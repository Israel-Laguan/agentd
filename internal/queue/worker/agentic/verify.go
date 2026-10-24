package agentic

import (
	"context"
	"errors"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// isBashFailure reports whether call is a shell command whose result did not pass.
func isBashFailure(call gateway.ToolCall, tr agenttools.ToolResult) bool {
	if call.Function.Name != "bash" {
		return false
	}
	switch tr.Status {
	case agenttools.ToolStatusError, agenttools.ToolStatusFatal, agenttools.ToolStatusTimeout:
		return true
	}
	return false
}

// noteBashOutcome records whether the latest shell command passed. A failed
// command leaves its result behind; a passing one clears it (B-082).
func (e *Engine) noteBashOutcome(call gateway.ToolCall, tr agenttools.ToolResult) {
	if call.Function.Name != "bash" {
		return
	}
	switch tr.Status {
	case agenttools.ToolStatusError, agenttools.ToolStatusFatal, agenttools.ToolStatusTimeout:
		e.lastBashFailure = truncateForReason(tr.Content)
	case agenttools.ToolStatusSuccess:
		e.lastBashFailure = ""
	}
}

// unmetCriteriaFinish stops a text-only finish while a success-criteria file the
// task names is missing, empty or no longer parses (B-039). The work is not
// verified, so it is not COMPLETED: it goes to a human like an unverified finish.
func (e *Engine) unmetCriteriaFinish(task models.Task, project models.Project, turnIndex int, budgetGuard *agentruntime.BudgetGuard, messages []gateway.PromptMessage) *agentruntime.LoopResult {
	err := e.host.VerifyPostConditions(task, project)
	if err == nil {
		return nil
	}
	meta := agentruntime.BuildLoopMeta(turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(messages), 0, err.Error(), "", "")
	meta.ErrorCode = agenttools.ToolErrorCodeUnverified
	return &agentruntime.LoopResult{Status: agentruntime.LoopToolFailure, Meta: meta}
}

// openBreakdownFinish stops a text-only finish of a parent whose breakdown
// subtasks are not all completed. The parent completes once they do, so the
// model cannot complete it early (T-055b gap 2). A failed read also stops the
// finish, but only open subtasks make it unverified: a read error carries no
// code, so the task is retried instead of handed to a human.
func (e *Engine) openBreakdownFinish(ctx context.Context, task models.Task, turnIndex int, budgetGuard *agentruntime.BudgetGuard, messages []gateway.PromptMessage) *agentruntime.LoopResult {
	err := e.host.VerifyBreakdownComplete(ctx, task)
	if err == nil {
		return nil
	}
	meta := agentruntime.BuildLoopMeta(turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(messages), 0, err.Error(), "", "")
	if errors.Is(err, models.ErrBreakdownIncomplete) {
		meta.ErrorCode = agenttools.ToolErrorCodeUnverified
	}
	return &agentruntime.LoopResult{Status: agentruntime.LoopToolFailure, Meta: meta}
}

// unverifiedFinish stops a text-only finish that follows a failed shell command:
// the work was never verified, so it is not COMPLETED. The model can fix the
// command, so the stop is tagged for a retry. It returns nil when the last
// command passed or nothing has run.
func (e *Engine) unverifiedFinish(turnIndex int, budgetGuard *agentruntime.BudgetGuard, messages []gateway.PromptMessage) *agentruntime.LoopResult {
	if e.lastBashFailure == "" {
		return nil
	}
	reason := "unverified: the last shell command failed and nothing passed after it: " + e.lastBashFailure
	meta := agentruntime.BuildLoopMeta(turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(messages), 0, reason, "bash", "")
	meta.ErrorCode = agenttools.ToolErrorCodeUnverifiedCommand
	return &agentruntime.LoopResult{Status: agentruntime.LoopToolFailure, Meta: meta}
}
