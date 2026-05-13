package config

import (
	"context"
	"net/http"
	"time"
)

type ProviderCheckResult struct {
	Available      bool
	Provider       string
	HordeAvailable bool
	LocalHealthy   bool
	HasAPIKey      bool
}

func CheckProviders(cfg GatewayConfig) ProviderCheckResult {
	result := ProviderCheckResult{}

	for _, name := range cfg.Order {
		switch name {
		case "openai":
			if cfg.OpenAI.APIKey != "" {
				result.Available = true
				result.Provider = "openai"
				result.HasAPIKey = true
				return result
			}
		case "anthropic":
			if cfg.Anthropic.APIKey != "" {
				result.Available = true
				result.Provider = "anthropic"
				result.HasAPIKey = true
				return result
			}
		case "ollama":
			if isOllamaHealthy(cfg.Ollama.BaseURL) {
				result.Available = true
				result.Provider = "ollama"
				result.LocalHealthy = true
				return result
			}
		case "llamacpp":
			if isLlamaCppHealthy(cfg.LlamaCpp.BaseURL) {
				result.Available = true
				result.Provider = "llamacpp"
				result.LocalHealthy = true
				return result
			}
		case "horde":
			result.HordeAvailable = true
		}
	}

	return result
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