package worker

import (
	agenthooks "agentd/internal/agent/hooks"
	wsession "agentd/internal/agent/session"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func buildWorkerHooks(
	opts WorkerOptions,
	toolExecutor *agenttools.ToolExecutor,
	sink models.EventSink,
	scrubber sandbox.Scrubber,
) *agenthooks.HookChain {
	base := opts.Hooks
	if base == nil {
		base = agenthooks.NewHookChain()
	}
	hooks := base.Clone()
	hooks.RegisterPre(agenthooks.SchemaValidationHook(agenttools.SchemaRegistryFromDefinitions(toolExecutor.Definitions())))
	if !opts.DisableCredentialDetection {
		hooks.RegisterPre(agenthooks.CredentialDetectionHook())
	}
	if len(opts.ToolCredentials) > 0 {
		store := wsession.NewEnvSecretStore(opts.ToolCredentials)
		hooks.RegisterPre(agenthooks.CredentialInjectionHook(store))
		hooks.RegisterSessionStart(agenthooks.CredentialValidationSessionHook(store))
	}
	hooks.PrependPost(agenthooks.ScrubResultHook(scrubber))
	hooks.RegisterPost(agenthooks.InjectionResistanceHook(agenthooks.ExternalToolsSet(opts.ExternalTools)))
	hooks.RegisterPost(agenthooks.AuditHook(sink, scrubber))
	return hooks
}
