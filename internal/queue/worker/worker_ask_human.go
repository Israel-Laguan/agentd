package worker

import (
	"context"
	"time"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
)

// runAskHumanTool is the body of the ask_human builtin. It runs inside tool
// dispatch, so pre-hooks can veto it and post-hooks and the dispatch audit see
// it. A valid question blocks the task; the agentic loop stops on the success
// result. A call that cannot block the task returns an error, so the model can
// try again.
func (w *Worker) runAskHumanTool(ctx context.Context, taskID string, call gateway.ToolCall, start time.Time) agenttools.ToolResult {
	fail := func(message string) agenttools.ToolResult {
		return agenttools.NonRetryableErrorResult(call.ID, message, "", time.Since(start).Milliseconds())
	}
	args, err := agenttools.ParseAskHumanArgs(call.Function.Arguments)
	if err != nil {
		return fail("invalid ask_human arguments: " + err.Error())
	}
	task, err := w.store.GetTask(ctx, taskID)
	if err != nil || task == nil {
		return fail("ask_human failed: task " + taskID + " not found")
	}
	if err := w.AskHuman(ctx, *task, args.Question, args.Options); err != nil {
		return fail("ask_human failed: " + err.Error())
	}
	return agenttools.SuccessResult(call.ID, "The question was sent to a human. The task is blocked until they answer.", time.Since(start).Milliseconds())
}
