package providers

import (
	"context"
	"fmt"

	"agentd/internal/gateway/spec"
)

// Backend is a concrete LLM provider implementation.
type Backend interface {
	Name() spec.Provider
	MaxInputChars() int
	Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error)
	Capabilities() Capabilities
}

// EmbedBackend supports embedding API calls (OpenAI-compatible providers).
type EmbedBackend interface {
	Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error)
}

// Capabilities represents the capabilities of a provider.
type Capabilities struct {
	SupportsChatTools bool
}

func providerName(cfg spec.ProviderConfig, fallback spec.Provider) spec.Provider {
	if cfg.Name != "" {
		return spec.Provider(cfg.Name)
	}
	if cfg.Type != "" {
		return spec.Provider(cfg.Type)
	}
	return fallback
}

// AppendFromConfig appends a provider built from cfg when the adapter Type is recognized.
func AppendFromConfig(backends []Backend, cfg spec.ProviderConfig) ([]Backend, error) {
	switch spec.Provider(cfg.Type) {
	case spec.ProviderOpenAI:
		return append(backends, NewOpenAI(cfg, nil)), nil
	case spec.ProviderAnthropic:
		return append(backends, NewAnthropic(cfg, nil)), nil
	case spec.ProviderOllama:
		return append(backends, NewOllama(cfg, nil)), nil
	case spec.ProviderLlamaCpp:
		return append(backends, NewLlamaCpp(cfg, nil)), nil
	case spec.ProviderHorde:
		return append(backends, NewHorde(cfg, nil)), nil
	case spec.ProviderGemini:
		// Gemini exposes an OpenAI-compatible endpoint; reuse the OpenAI backend.
		return append(backends, NewOpenAI(cfg, nil)), nil
	default:
		return backends, fmt.Errorf("unknown provider adapter %q", cfg.Type)
	}
}
