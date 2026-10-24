package worker

import (
	"fmt"

	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/models"
)

// withAllowlistHook adds AllowlistHook to taskHooks when the profile sets
// AllowedTools, creating the chain if needed. It runs after plugin hooks and
// before the approval gate, so a call outside the list is refused without asking
// a human.
func withAllowlistHook(taskHooks *agenthooks.HookChain, profile models.AgentProfile) *agenthooks.HookChain {
	allowed := agenttools.ProfileAllowlist(profile.AllowedTools)
	if allowed == nil {
		return taskHooks
	}
	if taskHooks == nil {
		taskHooks = agenthooks.NewHookChain()
	}
	taskHooks.RegisterPre(AllowlistHook(allowed))
	return taskHooks
}

// AllowlistHook vetoes a tool call whose name the profile's AllowedTools does
// not list. Filtering the offered list only hides a tool: a model that names
// one anyway, or is steered by a file it read, still reaches it. The veto runs
// before approval gates, so an unlisted call never asks a human either.
func AllowlistHook(allowed map[string]bool) agenthooks.PreHook {
	return agenthooks.PreHook{
		Name:   "allowed-tools",
		Policy: agenthooks.FailClosed,
		Fn: func(ctx agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			if agenttools.AllowsTool(allowed, ctx.ToolName) {
				return agenthooks.HookVerdict{}, nil
			}
			return agenthooks.HookVerdict{
				Veto:   true,
				Reason: fmt.Sprintf("tool %q is not in this profile's allowed_tools", ctx.ToolName),
			}, nil
		},
	}
}
