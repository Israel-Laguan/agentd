package config

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"agentd/internal/gateway"
)

const (
	healthModeAPIKey   = "api_key"
	healthModeOllama   = "ollama"
	healthModeLlamaCpp = "llamacpp"
	healthModeHorde    = "horde"
)

var allExplicitHealthModes = []string{
	healthModeAPIKey,
	healthModeOllama,
	healthModeLlamaCpp,
	healthModeHorde,
}

var knownHealthModes map[string]struct{}

func init() {
	knownHealthModes = make(map[string]struct{}, len(allExplicitHealthModes))
	for _, m := range allExplicitHealthModes {
		knownHealthModes[m] = struct{}{}
	}
}

func validHealthModesHint() string {
	modes := slices.Clone(allExplicitHealthModes)
	slices.Sort(modes)
	return strings.Join(modes, ", ")
}

var allKnownAdapters = []string{
	string(gateway.ProviderOpenAI),
	string(gateway.ProviderAnthropic),
	string(gateway.ProviderOllama),
	string(gateway.ProviderLlamaCpp),
	string(gateway.ProviderHorde),
}

func validAdaptersHint() string {
	adapters := slices.Clone(allKnownAdapters)
	slices.Sort(adapters)
	return strings.Join(adapters, ", ")
}

// normalizeAdapterType trims and lowercases a recognised adapter name.
func normalizeAdapterType(adapter string) (string, error) {
	t := strings.TrimSpace(strings.ToLower(adapter))
	if t == string(gateway.ProviderGemini) {
		return string(gateway.ProviderOpenAI), nil
	}
	switch gateway.Provider(t) {
	case gateway.ProviderOpenAI, gateway.ProviderAnthropic, gateway.ProviderOllama,
		gateway.ProviderLlamaCpp, gateway.ProviderHorde:
		return t, nil
	default:
		return "", fmt.Errorf("unknown adapter %q (valid: %s)", adapter, validAdaptersHint())
	}
}

// validateHealthMode returns an error when health is explicitly set to an
// unrecognised value. An empty string is valid (adapter default is used).
func validateHealthMode(health string) error {
	h := strings.TrimSpace(strings.ToLower(health))
	if h == "" {
		return nil
	}
	if _, ok := knownHealthModes[h]; !ok {
		return fmt.Errorf("unknown health mode %q (valid: %s)", h, validHealthModesHint())
	}
	return nil
}

type ProviderCheckResult struct {
	Available      bool
	Provider       string
	AdapterType    string
	HealthMode     string
	BaseURL        string
	HordeAvailable bool
	LocalHealthy   bool
	HasAPIKey      bool
}

// CheckProvidersOffline reports the first configured provider without network probes.
// Use for CLI hints where responsiveness matters more than live health.
func CheckProvidersOffline(cfg GatewayConfig) ProviderCheckResult {
	result := ProviderCheckResult{}
	configs, err := cfg.ProviderConfigs()
	if err != nil {
		slog.Warn("provider config error while checking availability", "err", err)
		return result
	}

	var hordeCandidate *gateway.ProviderConfig

	for i := range configs {
		provider := configs[i]
		mode := healthModeFor(provider)
		switch mode {
		case healthModeAPIKey:
			if r, ok := tryAPIKeyHealth(provider); ok {
				return r
			}
		case healthModeOllama, healthModeLlamaCpp:
			if provider.BaseURL != "" {
				return availableFromConfig(provider)
			}
		case healthModeHorde:
			result.HordeAvailable = true
			hordeCandidate = &configs[i]
		}
	}

	if hordeCandidate != nil && hordeCandidate.BaseURL != "" {
		r := availableFromConfig(*hordeCandidate)
		r.HordeAvailable = true
		return r
	}
	return result
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
		mode := healthModeFor(provider)
		switch mode {
		case healthModeAPIKey:
			if r, ok := tryAPIKeyHealth(provider); ok {
				return r
			}
		case healthModeOllama, healthModeLlamaCpp:
			if r, ok := tryLocalHealth(provider, mode); ok {
				return r
			}
		case healthModeHorde:
			result.HordeAvailable = true
			hordeCandidate = &configs[i]
		default:
			if mode != "" {
				slog.Debug("unknown provider health mode", "provider", provider.Name, "health", mode)
			}
		}
	}

	if hordeCandidate != nil && !result.Available && isHordeHealthy(hordeCandidate.BaseURL) {
		r := availableFromConfig(*hordeCandidate)
		r.HordeAvailable = true
		return r
	}
	return result
}

func healthModeFor(p gateway.ProviderConfig) string {
	if h := strings.TrimSpace(strings.ToLower(p.Health)); h != "" {
		return h
	}
	switch gateway.Provider(strings.TrimSpace(strings.ToLower(p.Adapter))) {
	case gateway.ProviderOpenAI, gateway.ProviderAnthropic, gateway.ProviderGemini:
		return healthModeAPIKey
	case gateway.ProviderOllama:
		return healthModeOllama
	case gateway.ProviderLlamaCpp:
		return healthModeLlamaCpp
	case gateway.ProviderHorde:
		return healthModeHorde
	default:
		return ""
	}
}

func availableFromConfig(p gateway.ProviderConfig) ProviderCheckResult {
	return ProviderCheckResult{
		Available:   true,
		Provider:    p.Name,
		AdapterType: p.Adapter,
		HealthMode:  healthModeFor(p),
		BaseURL:     p.BaseURL,
	}
}

func tryAPIKeyHealth(p gateway.ProviderConfig) (ProviderCheckResult, bool) {
	if p.APIKey == "" {
		return ProviderCheckResult{}, false
	}
	r := availableFromConfig(p)
	r.HasAPIKey = true
	return r, true
}

func tryLocalHealth(p gateway.ProviderConfig, mode string) (ProviderCheckResult, bool) {
	var healthy func(string) bool
	switch mode {
	case healthModeOllama:
		healthy = isOllamaHealthy
	case healthModeLlamaCpp:
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

// WarmupLLM confirms that the active LLM provider is reachable and can respond
// to a real request. For async providers (Horde), a lightweight HTTP heartbeat
// is used instead of a full generation round-trip.
//
// Returns an error if the provider is unreachable or returns an error response,
// which will block daemon startup.
func WarmupLLM(ctx context.Context, gw gateway.AIGateway, gatewayCfg GatewayConfig) error {
	result := CheckProviders(gatewayCfg)
	if !result.Available {
		return fmt.Errorf("no LLM provider available for warmup")
	}

	// Horde is async: a generate call can take minutes. Ping its heartbeat instead.
	if result.HealthMode == healthModeHorde {
		return warmupHorde(ctx, result.Provider, result.BaseURL)
	}

	slog.Debug("LLM warmup starting", "provider", result.Provider)
	warmupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	resp, err := gw.Generate(warmupCtx, gateway.AIRequest{
		Messages:  []gateway.PromptMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 64,
	})
	if err != nil {
		return fmt.Errorf("LLM warmup failed (provider=%s): %w", result.Provider, err)
	}
	slog.Info("LLM warmup OK", "provider", resp.ProviderUsed, "model", resp.ModelUsed)
	return nil
}

// warmupHorde pings the AI Horde heartbeat endpoint to confirm the service is
// reachable without queuing a full async generation job.
func warmupHorde(ctx context.Context, provider, baseURL string) error {
	slog.Debug("LLM warmup starting", "provider", provider, "mode", "heartbeat")
	client := &http.Client{Timeout: 5 * time.Second}
	url := baseURL + "/v2/status/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("horde warmup request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("horde warmup failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("horde heartbeat returned %d", resp.StatusCode)
	}
	slog.Info("LLM warmup OK", "provider", provider, "mode", "heartbeat")
	return nil
}
