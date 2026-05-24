package config

import (
	"context"
	"net/http"
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
		return result
	}

	var hordeCandidate *gateway.ProviderConfig

	for i := range configs {
		provider := configs[i]
		switch gateway.Provider(provider.Type) {
		case gateway.ProviderOpenAI, gateway.ProviderGemini:
			if provider.APIKey != "" {
				result.Available = true
				result.Provider = provider.Name
				result.AdapterType = provider.Type
				result.BaseURL = provider.BaseURL
				result.HasAPIKey = true
				return result
			}
		case gateway.ProviderAnthropic:
			if provider.APIKey != "" {
				result.Available = true
				result.Provider = provider.Name
				result.AdapterType = provider.Type
				result.BaseURL = provider.BaseURL
				result.HasAPIKey = true
				return result
			}
		case gateway.ProviderOllama:
			if isOllamaHealthy(provider.BaseURL) {
				result.Available = true
				result.Provider = provider.Name
				result.AdapterType = provider.Type
				result.BaseURL = provider.BaseURL
				result.LocalHealthy = true
				return result
			}
		case gateway.ProviderLlamaCpp:
			if isLlamaCppHealthy(provider.BaseURL) {
				result.Available = true
				result.Provider = provider.Name
				result.AdapterType = provider.Type
				result.BaseURL = provider.BaseURL
				result.LocalHealthy = true
				return result
			}
		case gateway.ProviderHorde:
			result.HordeAvailable = true
			hordeCandidate = &provider
		}
	}

	if hordeCandidate != nil && !result.Available && isHordeHealthy(hordeCandidate.BaseURL) {
		result.Available = true
		result.Provider = hordeCandidate.Name
		result.AdapterType = hordeCandidate.Type
		result.BaseURL = hordeCandidate.BaseURL
	}
	return result
}

func isHordeHealthy(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	url := baseURL + "/v2/status/heartbeat"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
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

func isLlamaCppHealthy(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	url := baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
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

func isOllamaHealthy(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	url := baseURL + "/api/tags"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
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
