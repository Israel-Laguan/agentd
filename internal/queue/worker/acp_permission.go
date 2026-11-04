package worker

import (
	"context"
	"time"

	"agentd/internal/agent/acp"
	agenthooks "agentd/internal/agent/hooks"
	"agentd/internal/models"
)

// ACPPermitter answers an ACP agent's permission requests for task with the
// chains a builtin tool call passes, in order: the task's hooks from
// MountAgenticHooks (the AllowedTools veto, then the approval gate, whose HUMAN
// subtask is the HITL step), then the worker-global ones (the denylist). The
// first chain that refuses decides; a gate that blocked the task suspends the
// ACP call. Nil chains are skipped, and no hooks allow the call: the driver
// has already refused package installs and unknown calls.
func ACPPermitter(task models.Task, chains ...*agenthooks.HookChain) acp.Permitter {
	return func(ctx context.Context, p acp.Permission) (acp.Verdict, error) {
		for _, chain := range chains {
			if chain == nil {
				continue
			}
			v := chain.RunPre(agenthooks.HookContext{
				ToolName:      p.Tool,
				Args:          p.Args,
				CallID:        p.ToolCallID,
				SessionID:     task.ID,
				ProjectID:     task.ProjectID,
				Timestamp:     time.Now(),
				TaskUpdatedAt: task.UpdatedAt,
				ExecCtx:       ctx,
			})
			reason := v.Reason
			if reason == "" {
				reason = v.Result
			}
			// A short-circuit stands in for running the tool, which an ACP agent
			// does itself, so it refuses the call like a veto.
			if v.Suspend || v.Veto || v.ShortCircuit {
				return acp.Verdict{Suspend: v.Suspend, Reason: reason}, nil
			}
		}
		return acp.Verdict{Allow: true}, nil
	}
}
