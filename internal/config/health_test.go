package config

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agentd/internal/gateway"
)

func TestCheckProviders_NoProviders(t *testing.T) {
	cfg := GatewayConfig{
		Order:  []string{"openai"},
		OpenAI: gateway.ProviderConfig{APIKey: "", Model: "gpt-4"},
	}
	result := CheckProviders(cfg)
	if result.Available {
		t.Error("expected Available to be false when no API key")
	}
}

func TestCheckProviders_HasOpenAIKey(t *testing.T) {
	cfg := GatewayConfig{
		Order:  []string{"openai"},
		OpenAI: gateway.ProviderConfig{APIKey: "sk-test", Model: "gpt-4"},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Error("expected Available to be true with OpenAI key")
	}
	if result.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", result.Provider)
	}
	if !result.HasAPIKey {
		t.Error("expected HasAPIKey to be true")
	}
}

func TestCheckProviders_HasAnthropicKey(t *testing.T) {
	cfg := GatewayConfig{
		Order:     []string{"anthropic"},
		Anthropic: gateway.ProviderConfig{APIKey: "sk-ant-test", Model: "claude-3-haiku"},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Error("expected Available to be true with Anthropic key")
	}
	if result.Provider != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", result.Provider)
	}
}

func TestCheckProviders_HordeAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/status/heartbeat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := GatewayConfig{
		Order: []string{"horde"},
		Horde: gateway.ProviderConfig{BaseURL: server.URL, APIKey: "0000000000", Model: ""},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Error("expected Available to be true when horde is the only provider in order")
	}
	if result.Provider != "horde" {
		t.Errorf("expected provider horde, got %s", result.Provider)
	}
	if !result.HordeAvailable {
		t.Error("expected HordeAvailable to be true")
	}
	if result.HealthMode != healthModeHorde {
		t.Errorf("expected HealthMode %q, got %q", healthModeHorde, result.HealthMode)
	}
}

func TestCheckProviders_HordeUnavailableWhenHeartbeatFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/status/heartbeat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := GatewayConfig{
		Order: []string{"horde"},
		Horde: gateway.ProviderConfig{BaseURL: server.URL, APIKey: "0000000000", Model: ""},
	}
	result := CheckProviders(cfg)
	if result.Available {
		t.Error("expected Available to be false when horde heartbeat is unhealthy")
	}
	if !result.HordeAvailable {
		t.Error("expected HordeAvailable to be true")
	}
}

// TestCheckProviders_OpenAICompatibleAdapterWithAPIKey verifies that any provider
// using the openai adapter with an API key is reported as available via healthModeAPIKey.
// Uses a synthetic name so no new test is needed when further OpenAI-compatible vendors
// are added via config.
func TestCheckProviders_OpenAICompatibleAdapterWithAPIKey(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"synth-openai-compat"},
		Providers: []gateway.ProviderConfig{{
			Name:    "synth-openai-compat",
			Adapter: "openai",
			APIKey:  "test-key",
			Model:   "test-model",
		}},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Error("expected Available to be true with openai-adapter API key")
	}
	if result.Provider != "synth-openai-compat" {
		t.Errorf("expected provider synth-openai-compat, got %s", result.Provider)
	}
	if !result.HasAPIKey {
		t.Error("expected HasAPIKey to be true")
	}
	if result.HealthMode != healthModeAPIKey {
		t.Errorf("expected HealthMode %q, got %q", healthModeAPIKey, result.HealthMode)
	}
}

// TestCheckProviders_GeminiKey is a regression guard for the legacy GatewayConfig.Gemini
// named field.  New OpenAI-compatible vendors should use GatewayConfig.Providers instead;
// see TestCheckProviders_OpenAICompatibleAdapterWithAPIKey for the general pattern.
func TestCheckProviders_GeminiKey(t *testing.T) {
	cfg := GatewayConfig{
		Order:  []string{"gemini"},
		Gemini: gateway.ProviderConfig{APIKey: "gemini-test-key", Model: "gemini-2.5-flash"},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Error("expected Available to be true with Gemini key")
	}
	if result.Provider != "gemini" {
		t.Errorf("expected provider gemini, got %s", result.Provider)
	}
	if !result.HasAPIKey {
		t.Error("expected HasAPIKey to be true")
	}
}

func TestCheckProviders_InvalidOrder(t *testing.T) {
	cfg := GatewayConfig{Order: []string{"nope"}}
	result := CheckProviders(cfg)
	if result.Available {
		t.Error("expected Available to be false when gateway.order references unknown provider")
	}
}

func TestCheckProviders_OrderPreference(t *testing.T) {
	cfg := GatewayConfig{
		Order:     []string{"openai", "anthropic"},
		OpenAI:    gateway.ProviderConfig{APIKey: "sk-openai", Model: "gpt-4"},
		Anthropic: gateway.ProviderConfig{APIKey: "sk-anthropic", Model: "claude-3"},
	}
	result := CheckProviders(cfg)
	if result.Provider != "openai" {
		t.Errorf("expected provider openai (first in order), got %s", result.Provider)
	}
}

func TestCheckProviders_HordeFallback(t *testing.T) {
	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ollamaSrv.Close()

	hordeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/status/heartbeat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer hordeSrv.Close()

	cfg := GatewayConfig{
		Order:  []string{"openai", "ollama", "horde"},
		OpenAI: gateway.ProviderConfig{APIKey: "", Model: "gpt-4"},
		Ollama: gateway.ProviderConfig{BaseURL: ollamaSrv.URL, Model: "llama3"},
		Horde:  gateway.ProviderConfig{BaseURL: hordeSrv.URL, APIKey: "0000000000", Model: ""},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Error("expected Available to be true when horde is the only viable provider")
	}
	if result.Provider != "horde" {
		t.Errorf("expected provider horde, got %s", result.Provider)
	}
	if !result.HordeAvailable {
		t.Error("expected HordeAvailable to be true as fallback")
	}
}

func TestCheckProviders_OllamaHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := GatewayConfig{
		Order:  []string{"ollama"},
		Ollama: gateway.ProviderConfig{BaseURL: server.URL, Model: "llama3"},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Error("expected Available to be true with healthy Ollama")
	}
	if result.Provider != "ollama" {
		t.Errorf("expected provider ollama, got %s", result.Provider)
	}
	if !result.LocalHealthy {
		t.Error("expected LocalHealthy to be true")
	}
}

func TestCheckProviders_LlamaCppHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := GatewayConfig{
		Order:    []string{"llamacpp"},
		LlamaCpp: gateway.ProviderConfig{BaseURL: server.URL, Model: "test.gguf"},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Error("expected Available to be true with healthy LlamaCpp")
	}
	if result.Provider != "llamacpp" {
		t.Errorf("expected provider llamacpp, got %s", result.Provider)
	}
	if !result.LocalHealthy {
		t.Error("expected LocalHealthy to be true")
	}
}

func TestCheckProviders_CustomProvider_AdapterDefaultOpenAI(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"poolside"},
		Providers: []gateway.ProviderConfig{{
			Name:   "poolside",
			Adapter: "openai",
			APIKey: "poolside-key",
			Model:  "poolside-model",
		}},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Fatal("expected Available to be true with adapter default api_key probe")
	}
	if result.HealthMode != healthModeAPIKey {
		t.Errorf("expected HealthMode %q, got %q", healthModeAPIKey, result.HealthMode)
	}
	if result.Provider != "poolside" {
		t.Errorf("expected provider poolside, got %s", result.Provider)
	}
}

func TestCheckProviders_CustomProvider_HealthAPIKey(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"poolside"},
		Providers: []gateway.ProviderConfig{{
			Name:   "poolside",
			Adapter: "openai",
			APIKey: "poolside-key",
			Model:  "poolside-model",
			Health: "api_key",
		}},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Fatal("expected Available to be true with custom provider health api_key")
	}
	if result.Provider != "poolside" {
		t.Errorf("expected provider poolside, got %s", result.Provider)
	}
	if !result.HasAPIKey {
		t.Error("expected HasAPIKey to be true")
	}
}

func TestCheckProviders_OfflineSkipsNetworkProbes(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"ollama", "openai"},
		Ollama: gateway.ProviderConfig{
			BaseURL: "http://127.0.0.1:1",
			Model:   "llama3",
		},
		OpenAI: gateway.ProviderConfig{APIKey: "", Model: "gpt-4"},
	}
	resultCh := make(chan ProviderCheckResult, 1)
	go func() { resultCh <- CheckProvidersOffline(cfg) }()
	select {
	case result := <-resultCh:
		if !result.Available {
			t.Errorf("expected offline check to report ollama configured, got Available=false")
		}
		if result.Provider != "ollama" {
			t.Errorf("Provider = %q, want ollama", result.Provider)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("CheckProvidersOffline blocked on unreachable local provider")
	}
}

func TestHealthModeFor_GeminiAdapterUncanonicalizedReturnsAPIKey(t *testing.T) {
	// healthModeFor is called in loadGatewayProviders before canonicalGatewayProvider
	// runs, so p.Adapter may still be "gemini" (not yet normalized to "openai").
	// It must return healthModeAPIKey so the missing-API-key warning fires correctly.
	p := gateway.ProviderConfig{
		Name:    "gemini",
		Adapter: "gemini",
		APIKey:  "",
	}
	if got := healthModeFor(p); got != healthModeAPIKey {
		t.Errorf("healthModeFor with adapter=\"gemini\" = %q, want %q", got, healthModeAPIKey)
	}
}

func TestCheckProviders_HealthAPIKey_OverridesAdapter(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"local"},
		Providers: []gateway.ProviderConfig{{
			Name:   "local",
			Adapter: "ollama",
			APIKey: "sk-override",
			Health: "api_key",
		}},
	}
	result := CheckProviders(cfg)
	if !result.Available {
		t.Fatal("expected Available to be true when health api_key overrides ollama adapter")
	}
	if !result.HasAPIKey {
		t.Error("expected HasAPIKey to be true")
	}
	if result.LocalHealthy {
		t.Error("expected LocalHealthy to be false for api_key health mode")
	}
}
