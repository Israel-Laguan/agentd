package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
)

type GatewayConfig struct {
	Order            []string
	WarmupEnabled    bool
	Providers        []gateway.ProviderConfig
	OpenAI           gateway.ProviderConfig
	Anthropic        gateway.ProviderConfig
	Ollama           gateway.ProviderConfig
	LlamaCpp         gateway.ProviderConfig
	Horde            gateway.ProviderConfig
	Gemini           gateway.ProviderConfig
	Truncation       TruncationConfig
	Truncator        TruncatorConfig
	MaxTasksPerPhase int
	MCPServers       []CapabilityManifest `json:"-"`
	// Routing is the validated agents: and stages: sections. It picks the agent
	// for each call; an empty table leaves every call on the cascade.
	Routing route.Table `json:"-"`
}

type TruncationConfig struct {
	Strategy       string
	HeadRatio      float64
	MaxInputChars  int
	StashThreshold int
}

type TruncatorConfig struct {
	Policy        string
	MaxInputChars int
}

func setGatewayDefaults(v *viper.Viper) {
	v.SetDefault("gateway.warmup_enabled", true)
	v.SetDefault("gateway.order", []string{"openai", "ollama"})
	v.SetDefault("gateway.openai.api_key", "")
	v.SetDefault("gateway.openai.base_url", "https://api.openai.com/v1")
	v.SetDefault("gateway.openai.model", "gpt-4o-mini")
	v.SetDefault("gateway.openai.max_input_chars", 0)
	v.SetDefault("gateway.openai.timeout", "5m")
	v.SetDefault("gateway.anthropic.api_key", "")
	v.SetDefault("gateway.anthropic.base_url", "https://api.anthropic.com")
	v.SetDefault("gateway.anthropic.model", "claude-3-haiku-20240307")
	v.SetDefault("gateway.anthropic.max_input_chars", 0)
	v.SetDefault("gateway.anthropic.timeout", "5m")
	v.SetDefault("gateway.ollama.base_url", "http://127.0.0.1:11434")
	v.SetDefault("gateway.ollama.model", "llama3:8b")
	v.SetDefault("gateway.ollama.max_input_chars", 0)
	v.SetDefault("gateway.ollama.timeout", "5m")
	v.SetDefault("gateway.llamacpp.base_url", "http://127.0.0.1:8080")
	v.SetDefault("gateway.llamacpp.model", "gpt-4")
	v.SetDefault("gateway.llamacpp.max_input_chars", 0)
	v.SetDefault("gateway.llamacpp.timeout", "5m")
	v.SetDefault("gateway.horde.base_url", "https://aihorde.net/api")
	v.SetDefault("gateway.horde.api_key", "0000000000")
	v.SetDefault("gateway.horde.model", "")
	v.SetDefault("gateway.horde.max_input_chars", 0)
	v.SetDefault("gateway.horde.timeout", "5m")
	v.SetDefault("gateway.horde.poll_interval", "4s")
	v.SetDefault("gateway.gemini.api_key", "")
	v.SetDefault("gateway.gemini.base_url", "https://generativelanguage.googleapis.com/v1beta/openai")
	v.SetDefault("gateway.gemini.model", "")
	v.SetDefault("gateway.gemini.max_input_chars", 0)
	v.SetDefault("gateway.gemini.timeout", "5m")
	v.SetDefault("gateway.max_tasks_per_phase", 7)
	v.SetDefault("gateway.truncator.policy", gateway.TruncatorPolicyHeadTail)
	v.SetDefault("gateway.truncator.max_input_chars", 12000)
	v.SetDefault("gateway.truncation.strategy", gateway.TruncationStrategyHeadTail)
	v.SetDefault("gateway.truncation.head_ratio", 0.5)
	v.SetDefault("gateway.truncation.max_input_chars", 12000)
	v.SetDefault("gateway.truncation.stash_threshold", 50000)
}

func loadGatewayConfig(v *viper.Viper, process, dotenv map[string]string) (GatewayConfig, error) {
	openAI, anthropic, ollama, llamaCpp, horde, gemini := loadGatewayProviderConfigs(v, process, dotenv)
	providers, err := loadGatewayProviders(v, process, dotenv)
	if err != nil {
		return GatewayConfig{}, err
	}
	mcpServers, err := loadMCPServers(v)
	if err != nil {
		return GatewayConfig{}, fmt.Errorf("gateway mcp_servers: %w", err)
	}
	cfg := GatewayConfig{
		Order:         v.GetStringSlice("gateway.order"),
		WarmupEnabled: v.GetBool("gateway.warmup_enabled"),
		Providers:     providers,
		OpenAI:        openAI,
		Anthropic:     anthropic,
		Ollama:        ollama,
		LlamaCpp:      llamaCpp,
		Horde:         horde,
		Gemini:        gemini,
		Truncation: TruncationConfig{
			Strategy:       v.GetString("gateway.truncation.strategy"),
			HeadRatio:      v.GetFloat64("gateway.truncation.head_ratio"),
			MaxInputChars:  v.GetInt("gateway.truncation.max_input_chars"),
			StashThreshold: v.GetInt("gateway.truncation.stash_threshold"),
		},
		Truncator: TruncatorConfig{
			Policy:        v.GetString("gateway.truncator.policy"),
			MaxInputChars: v.GetInt("gateway.truncator.max_input_chars"),
		},
		MaxTasksPerPhase: v.GetInt("gateway.max_tasks_per_phase"),
		MCPServers:       mcpServers,
	}
	if _, err := cfg.ProviderConfigs(); err != nil {
		return GatewayConfig{}, fmt.Errorf("gateway providers: %w", err)
	}
	if cfg.Routing, err = loadRouteTable(v); err != nil {
		return GatewayConfig{}, err
	}
	// Routed agents must name a provider that actually exists in the gateway
	// config; otherwise the stage route is silently ignored and the cascade
	// runs instead.
	byName, err := cfg.gatewayProvidersByName()
	if err != nil {
		return GatewayConfig{}, err
	}
	known := make(map[string]struct{}, len(byName))
	for name := range byName {
		known[strings.ToLower(name)] = struct{}{}
	}
	for name, agent := range cfg.Routing.Agents {
		if _, ok := known[strings.ToLower(agent.Provider)]; !ok {
			return GatewayConfig{}, fmt.Errorf("agents.%s: provider %q is not configured", name, agent.Provider)
		}
	}
	return cfg, nil
}

func (c GatewayConfig) ProviderConfigs() ([]gateway.ProviderConfig, error) {
	byName, err := c.gatewayProvidersByName()
	if err != nil {
		return nil, err
	}

	configs := make([]gateway.ProviderConfig, 0, len(c.Order))
	seenOrder := make(map[string]struct{}, len(c.Order))
	for _, name := range c.Order {
		if _, exists := seenOrder[name]; exists {
			return nil, fmt.Errorf("duplicate provider %q in gateway.order", name)
		}
		seenOrder[name] = struct{}{}
		cfg, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown provider %q in gateway.order", name)
		}
		normalizedType, err := normalizeAdapterType(cfg.Adapter)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
		cfg.Adapter = normalizedType
		if err := validateHealthMode(cfg.Health); err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

func (c TruncationConfig) StrategyImpl() gateway.TruncationStrategy {
	switch c.Strategy {
	case gateway.TruncationStrategyHeadTail:
		return gateway.HeadTailStrategy{HeadRatio: c.HeadRatio}
	default:
		return gateway.MiddleOutStrategy{}
	}
}

func (c GatewayConfig) TruncatorImpl(gw gateway.AIGateway, breaker gateway.BreakerChecker) gateway.Truncator {
	return gateway.NewTruncator(c.Truncator.Policy, c.Truncation.HeadRatio, gw, breaker)
}

func durationOrDefault(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
