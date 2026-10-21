package agentic

import (
	"fmt"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
)

// toolLoopStop ends the loop after a fatal tool result, a failure streak on
// one tool, or a no-progress repeat; nil means the loop goes on.
func (e *Engine) toolLoopStop(
	call gateway.ToolCall, tr agenttools.ToolResult, contextContent string, toolTracker *agenttools.ToolFailureTracker,
	turnIndex int, budgetGuard *agentruntime.BudgetGuard, messages *[]gateway.PromptMessage,
) *agentruntime.LoopResult {
	if failed, _ := toolTracker.Record(call.Function.Name, tr.Status); failed || tr.Status == agenttools.ToolStatusFatal {
		meta := agentruntime.BuildLoopMeta(
			turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(*messages), 0,
			contextContent, call.Function.Name, "",
		)
		if tr.Error != nil {
			meta.ErrorCode = tr.Error.Code
		}
		return &agentruntime.LoopResult{Status: agentruntime.LoopToolFailure, Meta: meta}
	}
	return e.noProgressStop(call, contextContent, turnIndex, budgetGuard, messages)
}

// noProgressStop ends the loop when this exact call has now returned this
// exact result DefaultNoProgressRepeats times (B-081): a missing toolchain
// gave the same "command not found" every cycle until the wall timeout.
func (e *Engine) noProgressStop(
	call gateway.ToolCall, result string, turnIndex int,
	budgetGuard *agentruntime.BudgetGuard, messages *[]gateway.PromptMessage,
) *agentruntime.LoopResult {
	if e.repeats == nil {
		e.repeats = agenttools.NewRepeatTracker(agenttools.DefaultNoProgressRepeats)
	}
	stuck, count := e.repeats.Record(call.Function.Name, call.Function.Arguments, result)
	if !stuck {
		return nil
	}
	reason := fmt.Sprintf("no progress: the same %s call returned the same result %d times (last: %s)",
		call.Function.Name, count, truncateForReason(result))
	meta := agentruntime.BuildLoopMeta(turnIndex, budgetGuard.Usage(), agentcontext.TotalChars(*messages), 0, reason, call.Function.Name, "")
	meta.ErrorCode = agenttools.ToolErrorCodeNoProgress
	return &agentruntime.LoopResult{Status: agentruntime.LoopToolFailure, Meta: meta}
}

// truncateForReason keeps a stop reason readable when the tool result is long.
func truncateForReason(result string) string {
	const limit = 200
	if runes := []rune(result); len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return result
}
