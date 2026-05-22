package config

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
	cfg := GatewayConfig{
		Order: []string{"horde"},
		Horde: gateway.ProviderConfig{APIKey: "0000000000", Model: ""},
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
}

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := GatewayConfig{
		Order:  []string{"openai", "ollama", "horde"},
		OpenAI: gateway.ProviderConfig{APIKey: "", Model: "gpt-4"},
		Ollama: gateway.ProviderConfig{BaseURL: server.URL, Model: "llama3"},
		Horde:  gateway.ProviderConfig{APIKey: "0000000000", Model: ""},
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
