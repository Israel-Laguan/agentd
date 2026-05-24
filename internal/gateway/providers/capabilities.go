package providers

import (
	"strings"

	"agentd/internal/gateway/spec"
)

// SupportsChatTools reports whether the provider adapter type supports chat tool
// round-tripping. Values match each backend's Capabilities() implementation.
// Custom provider names (e.g. poolside) are not recognized here; use
// gateway.ProviderSupportsChatTools with a configured Router instead.
func SupportsChatTools(provider string) bool {
	switch spec.Provider(strings.ToLower(strings.TrimSpace(provider))) {
	case spec.ProviderOpenAI, spec.ProviderAnthropic:
		return true
	default:
		return false
	}
}
