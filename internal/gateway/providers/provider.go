package providers

import (
	"context"

	"agentd/internal/gateway/spec"
)

// Backend is a concrete LLM provider implementation.
type Backend interface {
	Name() spec.Provider
	MaxInputChars() int
	Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error)
}

// AppendFromConfig appends a provider built from cfg when Type is recognized.
func AppendFromConfig(backends []Backend, cfg spec.ProviderConfig) []Backend {
	switch spec.Provider(cfg.Type) {
	case spec.ProviderOpenAI:
		return append(backends, NewOpenAI(cfg, nil))
	case spec.ProviderAnthropic:
		return append(backends, NewAnthropic(cfg, nil))
	case spec.ProviderOllama:
		return append(backends, NewOllama(cfg, nil))
	case spec.ProviderLlamaCpp:
		return append(backends, NewLlamaCpp(cfg, nil))
	case spec.ProviderHorde:
		return append(backends, NewHorde(cfg, nil))
	default:
		return backends
	}
}
