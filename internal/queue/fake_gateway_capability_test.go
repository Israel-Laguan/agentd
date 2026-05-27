package queue

// ProviderSupportsChatTools enables fakeGateway to satisfy the optional
// chatToolsChecker interface. Capability is resolved via a real router built from
// ProviderConfig, matching production behavior.
func (g *fakeGateway) ProviderSupportsChatTools(provider string) bool {
	return providerSupportsChatToolsViaRouter(provider)
}
