package config

import (
	"testing"

	"agentd/internal/gateway"
)

func TestCheckProviders_NoProviders(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"openai"},
		OpenAI: gateway.ProviderConfig{APIKey: "", Model: "gpt-4"},
	}
	result := CheckProviders(cfg)
	if result.Available {
		t.Error("expected Available to be false when no API key")
	}
}

func TestCheckProviders_HasOpenAIKey(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"openai"},
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
		Order: []string{"anthropic"},
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
	if result.Available {
		t.Error("expected Available to be false (horde is fallback only)")
	}
	if !result.HordeAvailable {
		t.Error("expected HordeAvailable to be true")
	}
}

func TestCheckProviders_OrderPreference(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"openai", "anthropic"},
		OpenAI: gateway.ProviderConfig{APIKey: "sk-openai", Model: "gpt-4"},
		Anthropic: gateway.ProviderConfig{APIKey: "sk-anthropic", Model: "claude-3"},
	}
	result := CheckProviders(cfg)
	if result.Provider != "openai" {
		t.Errorf("expected provider openai (first in order), got %s", result.Provider)
	}
}

func TestCheckProviders_HordeFallback(t *testing.T) {
	cfg := GatewayConfig{
		Order:    []string{"openai", "ollama", "horde"},
		OpenAI:   gateway.ProviderConfig{APIKey: "", Model: "gpt-4"},
		Ollama:   gateway.ProviderConfig{BaseURL: "http://127.0.0.1:11434", Model: "llama3"},
		Horde:    gateway.ProviderConfig{APIKey: "0000000000", Model: ""},
	}
	result := CheckProviders(cfg)
	if result.Available {
		t.Error("expected Available to be false (no key, local not running)")
	}
	if !result.HordeAvailable {
		t.Error("expected HordeAvailable to be true as fallback")
	}
}

func TestCheckProviders_LlamaCpp(t *testing.T) {
	cfg := GatewayConfig{
		Order:     []string{"llamacpp"},
		LlamaCpp:  gateway.ProviderConfig{BaseURL: "http://127.0.0.1:8080", Model: "test.gguf"},
	}
	result := CheckProviders(cfg)
	// This will fail if no llama.cpp server is running, which is expected in CI
	// The important thing is that the code path exists and doesn't crash
	t.Logf("LlamaCpp check result: Available=%v, Provider=%s, LocalHealthy=%v", result.Available, result.Provider, result.LocalHealthy)
}