package providers

import (
	"strings"

	"agentd/internal/gateway/spec"
)

// SupportsChatTools reports whether the provider type supports chat tool
// round-tripping. Values match each backend's Capabilities() implementation.
func SupportsChatTools(provider string) bool {
	switch spec.Provider(strings.ToLower(strings.TrimSpace(provider))) {
	case spec.ProviderOpenAI, spec.ProviderAnthropic:
		return true
	default:
		return false
	}
}
