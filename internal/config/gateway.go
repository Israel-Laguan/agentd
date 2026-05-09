package config

import (
	"os"
	"time"

	"agentd/internal/gateway"

	"github.com/spf13/viper"
)

type GatewayConfig struct {
	Order            []string
	OpenAI           gateway.ProviderConfig
	Anthropic        gateway.ProviderConfig
	Ollama           gateway.ProviderConfig
	LlamaCpp         gateway.ProviderConfig
	Horde            gateway.ProviderConfig
	Truncation       TruncationConfig
	Truncator        TruncatorConfig
	MaxTasksPerPhase int
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
	v.SetDefault("gateway.order", []string{"openai", "ollama"})
	v.SetDefault("gateway.openai.base_url", "https://api.openai.com/v1")
	v.SetDefault("gateway.openai.model", "gpt-4o-mini")
	v.SetDefault("gateway.openai.max_input_chars", 0)
	v.SetDefault("gateway.openai.timeout", "5m")
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
	v.SetDefault("gateway.max_tasks_per_phase", 7)
	v.SetDefault("gateway.truncator.policy", gateway.TruncatorPolicyHeadTail)
	v.SetDefault("gateway.truncator.max_input_chars", 12000)
	v.SetDefault("gateway.truncation.strategy", gateway.TruncationStrategyHeadTail)
	v.SetDefault("gateway.truncation.head_ratio", 0.5)
	v.SetDefault("gateway.truncation.max_input_chars", 12000)
	v.SetDefault("gateway.truncation.stash_threshold", 50000)
}

func loadGatewayConfig(v *viper.Viper) GatewayConfig {
	openAIKey := v.GetString("gateway.openai.api_key")
	if openAIKey == "" {
		openAIKey = os.Getenv("OPENAI_API_KEY")
	}
	anthropicKey := v.GetString("gateway.anthropic.api_key")
	if anthropicKey == "" {
		anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	return GatewayConfig{
		Order: v.GetStringSlice("gateway.order"),
		OpenAI: gateway.ProviderConfig{
			Type: "openai", BaseURL: v.GetString("gateway.openai.base_url"),
			APIKey: openAIKey, Model: v.GetString("gateway.openai.model"),
			MaxInputChars: v.GetInt("gateway.openai.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.openai.timeout"), 5*time.Minute),
		},
		Anthropic: gateway.ProviderConfig{
			Type: "anthropic", BaseURL: v.GetString("gateway.anthropic.base_url"),
			APIKey: anthropicKey, Model: v.GetString("gateway.anthropic.model"),
			MaxInputChars: v.GetInt("gateway.anthropic.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.anthropic.timeout"), 5*time.Minute),
		},
		Ollama: gateway.ProviderConfig{
			Type: "ollama", BaseURL: v.GetString("gateway.ollama.base_url"),
			Model:         v.GetString("gateway.ollama.model"),
			MaxInputChars: v.GetInt("gateway.ollama.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.ollama.timeout"), 5*time.Minute),
		},
		LlamaCpp: gateway.ProviderConfig{
			Type: "llamacpp", BaseURL: v.GetString("gateway.llamacpp.base_url"),
			Model:         v.GetString("gateway.llamacpp.model"),
			MaxInputChars: v.GetInt("gateway.llamacpp.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.llamacpp.timeout"), 5*time.Minute),
		},
		Horde: gateway.ProviderConfig{
			Type: "horde", BaseURL: v.GetString("gateway.horde.base_url"),
			APIKey: v.GetString("gateway.horde.api_key"), Model: v.GetString("gateway.horde.model"),
			MaxInputChars: v.GetInt("gateway.horde.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.horde.timeout"), 5*time.Minute),
			PollInterval:  durationOrDefault(v.GetDuration("gateway.horde.poll_interval"), 4*time.Second),
		},
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
	}
}

func (c GatewayConfig) ProviderConfigs() []gateway.ProviderConfig {
	configs := make([]gateway.ProviderConfig, 0, len(c.Order))
	for _, name := range c.Order {
		if name == "openai" {
			configs = append(configs, c.OpenAI)
		}
		if name == "anthropic" {
			configs = append(configs, c.Anthropic)
		}
		if name == "ollama" {
			configs = append(configs, c.Ollama)
		}
		if name == "llamacpp" {
			configs = append(configs, c.LlamaCpp)
		}
		if name == "horde" {
			configs = append(configs, c.Horde)
		}
	}
	return configs
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
