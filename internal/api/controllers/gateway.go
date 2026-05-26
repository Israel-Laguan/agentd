package controllers

import (
	"net/http"

	"agentd/internal/api/httpx"
)

// GatewayHandler exposes metadata about configured gateway providers.
// Configs holds only the wire-safe summary; API keys and base URLs are
// stripped at construction time in server.NewHandler.
type GatewayHandler struct {
	Configs []ProviderEntry
}

// ProviderEntry is the JSON wire shape for a single provider entry.
type ProviderEntry struct {
	Name    string   `json:"name"`
	Adapter string   `json:"adapter"`
	Models  []string `json:"models"`
}

// List handles GET /api/v1/gateway/providers.
// It returns the list of providers configured in the daemon, with their
// adapter type and available models. Returns an empty array when no
// providers are configured.
func (h GatewayHandler) List(w http.ResponseWriter, r *http.Request) {
	entries := h.Configs
	if entries == nil {
		entries = []ProviderEntry{}
	}
	httpx.WriteSuccess(w, http.StatusOK, entries, nil)
}
