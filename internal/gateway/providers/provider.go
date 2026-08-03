package providers

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

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

// ToolProber can refine SupportsChatTools with a runtime tool-call probe.
type ToolProber interface {
	ProbeTools(context.Context) bool
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

// canonicalAdapter normalizes name/adapter fields and maps the legacy gemini adapter alias
// to openai while preserving gemini as the provider name when no explicit name is set.
func canonicalAdapter(cfg spec.ProviderConfig) spec.ProviderConfig {
	cfg.Name = strings.ToLower(strings.TrimSpace(cfg.Name))
	cfg.Adapter = strings.ToLower(strings.TrimSpace(cfg.Adapter))
	if cfg.Adapter == "" {
		cfg.Adapter = cfg.Name
	}
	if cfg.Name == "" {
		cfg.Name = cfg.Adapter
	}
	if cfg.Adapter == string(spec.ProviderGemini) {
		cfg.Adapter = string(spec.ProviderOpenAI)
		if cfg.Name == string(spec.ProviderGemini) || cfg.Name == "" {
			cfg.Name = string(spec.ProviderGemini)
		}
	}
	return cfg
}

// optionDuration retrieves a time.Duration from an adapter options map.
// val may be a time.Duration (set programmatically) or a string accepted by
// time.ParseDuration (as decoded from YAML). Falls back to def on missing key
// or unparseable value, logging an error in the latter case.
func optionDuration(opts map[string]any, key string, def time.Duration) time.Duration {
	val, ok := opts[key]
	if !ok {
		return def
	}
	switch v := val.(type) {
	case time.Duration:
		return v
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			slog.Error("option: invalid duration", "key", key, "value", v, "err", err)
			return def
		}
		return d
	default:
		slog.Error("option: expected duration or string", "key", key, "type", fmt.Sprintf("%T", val))
		return def
	}
}

// optionBool retrieves a bool from an adapter options map.
// val may be a bool or a string accepted by strconv.ParseBool (as decoded from
// YAML). Falls back to false on missing key or unparseable value, logging an
// error in the latter case.
func optionBool(opts map[string]any, key string) bool {
	val, ok := opts[key]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(v)
		if err != nil {
			slog.Error("option: invalid bool", "key", key, "value", v, "err", err)
			return false
		}
		return b
	default:
		slog.Error("option: expected bool or string", "key", key, "type", fmt.Sprintf("%T", val))
		return false
	}
}

// warnUnknownOptions logs a slog.Warn for each key in opts not present in known.
func warnUnknownOptions(adapter string, known []string, opts map[string]any) {
	if len(opts) == 0 {
		return
	}
	knownSet := make(map[string]struct{}, len(known))
	for _, k := range known {
		knownSet[k] = struct{}{}
	}
	for k := range opts {
		if _, ok := knownSet[k]; !ok {
			slog.Warn("unknown provider option ignored", "adapter", adapter, "key", k)
		}
	}
}

// AppendFromConfig appends a provider built from cfg when the adapter is recognized.
// When Adapter is empty it defaults to Name, preserving backward compatibility for
// entries that identify both vendor and wire protocol with a single name.
func AppendFromConfig(backends []Backend, cfg spec.ProviderConfig) ([]Backend, error) {
	cfg = canonicalAdapter(cfg)
	adapter := cfg.Adapter
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
	default:
		return backends, fmt.Errorf("unknown provider adapter %q", adapter)
	}
}
