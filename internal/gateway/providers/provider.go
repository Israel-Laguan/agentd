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
	if cfg.Adapter != "" {
		return spec.Provider(cfg.Adapter)
	}
	return fallback
}

func chatToolsCapability(cfg spec.ProviderConfig, adapterDefault bool) bool {
	if cfg.Capabilities.ChatTools != nil {
		return *cfg.Capabilities.ChatTools
	}
	return adapterDefault
}

func capabilitiesFromConfig(cfg spec.ProviderConfig, adapterDefault bool) Capabilities {
	return Capabilities{SupportsChatTools: chatToolsCapability(cfg, adapterDefault)}
}

// AppendFromConfig appends a provider built from cfg when the adapter is recognized.
// When Adapter is empty it defaults to Name, preserving backward compatibility for
// entries that identify both vendor and wire protocol with a single name.
func AppendFromConfig(backends []Backend, cfg spec.ProviderConfig) ([]Backend, error) {
	adapter := cfg.Adapter
	if adapter == "" {
		adapter = string(cfg.Name)
	}
	switch spec.Provider(adapter) {
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
		return backends, fmt.Errorf("unknown provider adapter %q", adapter)
	}
}
