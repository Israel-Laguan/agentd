package agentic

import (
	"context"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// isAskHumanCall reports whether call is the ask_human builtin the profile
// allows. A call the allowlist refuses goes to dispatch, whose hook vetoes it.
func isAskHumanCall(profile models.AgentProfile, call gateway.ToolCall) bool {
	if call.Function.Name != agenttools.ToolNameAskHuman {
		return false
	}
	return agenttools.AllowsTool(agenttools.ProfileAllowlist(profile.AllowedTools), call.Function.Name)
}

// askHuman runs an ask_human call. A valid question blocks the task for a
// human, and the loop stops (suspended). A call that cannot block the task
// returns an error result, so the model can try again.
func (e *Engine) askHuman(ctx context.Context, task models.Task, call gateway.ToolCall) (agenttools.ToolResult, bool) {
	args, err := agenttools.ParseAskHumanArgs(call.Function.Arguments)
	if err != nil {
		return agenttools.NonRetryableErrorResult(call.ID, "invalid ask_human arguments: "+err.Error(), "", 0), false
	}
	if err := e.host.AskHuman(ctx, task, args.Question, args.Options); err != nil {
		return agenttools.NonRetryableErrorResult(call.ID, "ask_human failed: "+err.Error(), "", 0), false
	}
	return agenttools.SuccessResult(call.ID, "The question was sent to a human. The task is blocked until they answer.", 0), true
}
