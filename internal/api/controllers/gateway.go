package controllers

import (
	"net/http"

	"agentd/internal/api/httpx"
	"agentd/internal/gateway/spec"
)

// GatewayHandler exposes metadata about configured gateway providers.
type GatewayHandler struct {
	Configs []spec.ProviderConfig
}

// providerEntry is the JSON wire shape for a single provider entry.
type providerEntry struct {
	Name    string   `json:"name"`
	Adapter string   `json:"adapter"`
	Models  []string `json:"models"`
}

// List handles GET /api/v1/gateway/providers.
// It returns the list of providers configured in the daemon, with their
// adapter type and available models. Returns an empty array when no
// providers are configured.
func (h GatewayHandler) List(w http.ResponseWriter, r *http.Request) {
	entries := make([]providerEntry, 0, len(h.Configs))
	for _, cfg := range h.Configs {
		models := make([]string, 0, 1)
		if cfg.Model != "" {
			models = append(models, cfg.Model)
		}
		entries = append(entries, providerEntry{
			Name:    cfg.Name,
			Adapter: cfg.Adapter,
			Models:  models,
		})
	}
	httpx.WriteSuccess(w, http.StatusOK, entries, nil)
}
