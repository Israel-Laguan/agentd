package queue

import "strings"

// ProviderSupportsChatTools enables fakeGateway to satisfy the optional
// chatToolsChecker interface so that gateway.ProviderSupportsChatTools returns
// the correct value during agentic worker tests in the queue package.
// Mirrors the real router's per-provider logic: openai, anthropic, gemini are
// tool-capable; everything else (ollama, unknown, empty) is not.
func (g *fakeGateway) ProviderSupportsChatTools(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "anthropic", "gemini":
		return true
	default:
		return false
	}
}
