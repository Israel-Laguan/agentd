package worker

import agenthooks "agentd/internal/agent/hooks"

type FailurePolicy = agenthooks.FailurePolicy
type HookVerdict = agenthooks.HookVerdict
type HookContext = agenthooks.HookContext
type PreHook = agenthooks.PreHook
type PostHook = agenthooks.PostHook
type SessionStartHook = agenthooks.SessionStartHook
type HookChain = agenthooks.HookChain
type ResultCache = agenthooks.ResultCache
type RateLimitCounter = agenthooks.RateLimitCounter
type RateLimitStore = agenthooks.RateLimitStore

const (
	FailOpen   = agenthooks.FailOpen
	FailClosed = agenthooks.FailClosed

	externalContentInstruction = agenthooks.ExternalContentInstruction
)

var (
	NewHookChain = agenthooks.NewHookChain

	SchemaValidationHook            = agenthooks.SchemaValidationHook
	CredentialDetectionHook         = agenthooks.CredentialDetectionHook
	CredentialInjectionHook         = agenthooks.CredentialInjectionHook
	CredentialValidationSessionHook = agenthooks.CredentialValidationSessionHook
	ScrubResultHook                 = agenthooks.ScrubResultHook
	InjectionResistanceHook         = agenthooks.InjectionResistanceHook
	AuditHook                       = agenthooks.AuditHook
	CacheLookupHook                 = agenthooks.CacheLookupHook
	CacheStoreHook                  = agenthooks.CacheStoreHook
	NewResultCache                  = agenthooks.NewResultCache
	DryRunHook                      = agenthooks.DryRunHook
	RateLimitHook                   = agenthooks.RateLimitHook
	resolveLimit                    = agenthooks.ResolveLimit
	NewRateLimitStore               = agenthooks.NewRateLimitStore
	DenylistHook                    = agenthooks.DenylistHook
	cacheKey                        = agenthooks.CacheKey
	canonicalizeArgs                = agenthooks.CanonicalizeArgs

	externalToolsSet         = agenthooks.ExternalToolsSet
	isExternalTool           = agenthooks.IsExternalTool
	wrapExternalContent      = agenthooks.WrapExternalContent
)
