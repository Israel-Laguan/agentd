package worker_test

import "strings"

// ProviderSupportsChatTools enables workerTestGateway to satisfy the optional
// chatToolsChecker interface. It mirrors the real router's per-provider logic
// so that feature scenarios that set an unsupported provider (e.g. "ollama")
// correctly fall back to the legacy path.
func (g *workerTestGateway) ProviderSupportsChatTools(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "anthropic", "gemini":
		return true
	default:
		return false
	}
}
