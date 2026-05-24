package worker

// worker_mock_gateway_capability_test.go adds ProviderSupportsChatTools to every
// test-only gateway type that must allow the worker's providerSupportsAgentic check
// to pass. Because gateway.ProviderSupportsChatTools now uses an optional interface
// (chatToolsChecker) instead of a static string switch, any gateway that is not a
// *routing.Router must implement ProviderSupportsChatTools itself.
//
// Routing-decision tests (routingTestGateway) implement proper per-provider logic.
// All other agentic-loop test doubles return true for any non-empty provider name,
// since those tests exercise loop mechanics, not provider selection.

import "strings"

// routingTestGateway: mirrors production routing — openai, anthropic, gemini are
// tool-capable; everything else (ollama, unknown, empty) falls back to legacy.
func (g *routingTestGateway) ProviderSupportsChatTools(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "anthropic", "gemini":
		return true
	default:
		return false
	}
}

// sequenceGateway: used for agentic loop integration tests (not routing tests).
func (g *sequenceGateway) ProviderSupportsChatTools(provider string) bool {
	return strings.TrimSpace(provider) != ""
}

// maxIterationsGateway: used for max-iterations and turn-limit tests.
func (m *maxIterationsGateway) ProviderSupportsChatTools(provider string) bool {
	return strings.TrimSpace(provider) != ""
}

// tokenUsageGateway: used for budget / token-accounting tests.
func (g *tokenUsageGateway) ProviderSupportsChatTools(provider string) bool {
	return strings.TrimSpace(provider) != ""
}

// planningSequenceGateway: used for agentic planning tests.
func (g *planningSequenceGateway) ProviderSupportsChatTools(provider string) bool {
	return strings.TrimSpace(provider) != ""
}

// batchTrackingGateway: used for batch tests where a malformed slot re-runs as
// a single agentic task.
func (g *batchTrackingGateway) ProviderSupportsChatTools(provider string) bool {
	return strings.TrimSpace(provider) != ""
}

// elicitationSequenceGateway: used for elicitor / HITL / clarification tests.
func (g *elicitationSequenceGateway) ProviderSupportsChatTools(provider string) bool {
	return strings.TrimSpace(provider) != ""
}
