package worker

import (
	"agentd/internal/models"
	"agentd/internal/sandbox"
	wsession "agentd/internal/queue/worker/session"
)

func buildWorkerHooks(
	opts WorkerOptions,
	toolExecutor *ToolExecutor,
	sink models.EventSink,
	scrubber sandbox.Scrubber,
) *HookChain {
	base := resolveHooks(opts.Hooks)
	hooks := base.Clone()
	hooks.RegisterPre(SchemaValidationHook(SchemaRegistryFromDefinitions(toolExecutor.Definitions())))
	if !opts.DisableCredentialDetection {
		hooks.RegisterPre(CredentialDetectionHook())
	}
	if len(opts.ToolCredentials) > 0 {
		store := wsession.NewEnvSecretStore(opts.ToolCredentials)
		hooks.RegisterPre(CredentialInjectionHook(store))
		hooks.RegisterSessionStart(CredentialValidationSessionHook(store))
	}
	hooks.PrependPost(ScrubResultHook(scrubber))
	hooks.RegisterPost(InjectionResistanceHook(externalToolsSet(opts.ExternalTools)))
	hooks.RegisterPost(AuditHook(sink, scrubber))
	return hooks
}
