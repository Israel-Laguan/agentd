package config

import (
	"context"
	"net/http"
	"log/slog"
	"time"

	"agentd/internal/gateway"
)

type ProviderCheckResult struct {
	Available      bool
	Provider       string
	AdapterType    string
	BaseURL        string
	HordeAvailable bool
	LocalHealthy   bool
	HasAPIKey      bool
}

func CheckProviders(cfg GatewayConfig) ProviderCheckResult {
	result := ProviderCheckResult{}
	configs, err := cfg.ProviderConfigs()
	if err != nil {
		slog.Warn("provider config error while checking availability", "err", err)
		return result
	}

	var hordeCandidate *gateway.ProviderConfig

	for i := range configs {
		provider := configs[i]
		if r, ok := tryKeyBackedProvider(provider); ok {
			return r
		}
		if r, ok := tryLocalProvider(provider); ok {
			return r
		}
		if gateway.Provider(provider.Type) == gateway.ProviderHorde {
			result.HordeAvailable = true
			hordeCandidate = &configs[i]
		}
	}

	if hordeCandidate != nil && !result.Available && isHordeHealthy(hordeCandidate.BaseURL) {
		r := availableFromConfig(*hordeCandidate)
		r.HordeAvailable = true
		return r
	}
	return result
}

func availableFromConfig(p gateway.ProviderConfig) ProviderCheckResult {
	return ProviderCheckResult{
		Available:   true,
		Provider:    p.Name,
		AdapterType: p.Type,
		BaseURL:     p.BaseURL,
	}
}

func tryKeyBackedProvider(p gateway.ProviderConfig) (ProviderCheckResult, bool) {
	switch gateway.Provider(p.Type) {
	case gateway.ProviderOpenAI, gateway.ProviderGemini, gateway.ProviderAnthropic:
		if p.APIKey == "" {
			return ProviderCheckResult{}, false
		}
		r := availableFromConfig(p)
		r.HasAPIKey = true
		return r, true
	default:
		return ProviderCheckResult{}, false
	}
}

func tryLocalProvider(p gateway.ProviderConfig) (ProviderCheckResult, bool) {
	var healthy func(string) bool
	switch gateway.Provider(p.Type) {
	case gateway.ProviderOllama:
		healthy = isOllamaHealthy
	case gateway.ProviderLlamaCpp:
		healthy = isLlamaCppHealthy
	default:
		return ProviderCheckResult{}, false
	}
	if !healthy(p.BaseURL) {
		return ProviderCheckResult{}, false
	}
	r := availableFromConfig(p)
	r.LocalHealthy = true
	return r, true
}

func isHordeHealthy(baseURL string) bool {
	return isGETHealthy(baseURL, "/v2/status/heartbeat")
}

func isLlamaCppHealthy(baseURL string) bool {
	return isGETHealthy(baseURL, "/v1/models")
}

func isOllamaHealthy(baseURL string) bool {
	return isGETHealthy(baseURL, "/api/tags")
}

func isGETHealthy(baseURL, path string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+path, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}
