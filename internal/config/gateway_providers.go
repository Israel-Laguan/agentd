package config

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/viper"

	"agentd/internal/gateway"
)

func apiKeyFromConfigOrEnv(v *viper.Viper, process, dotenv map[string]string, configKey, envVar string) string {
	if key := v.GetString(configKey); key != "" {
		return key
	}
	return envLookup(process, dotenv, envVar)
}

func loadGatewayProviderConfigs(v *viper.Viper, process, dotenv map[string]string) (
	openAI, anthropic, ollama, llamaCpp, horde, gemini gateway.ProviderConfig,
) {
	openAIKey := apiKeyFromConfigOrEnv(v, process, dotenv, "gateway.openai.api_key", "OPENAI_API_KEY")
	anthropicKey := apiKeyFromConfigOrEnv(v, process, dotenv, "gateway.anthropic.api_key", "ANTHROPIC_API_KEY")
	geminiKey := apiKeyFromConfigOrEnv(v, process, dotenv, "gateway.gemini.api_key", "GEMINI_API_KEY")
	return gateway.ProviderConfig{
			Adapter: "openai", BaseURL: v.GetString("gateway.openai.base_url"),
			APIKey: openAIKey, Model: v.GetString("gateway.openai.model"),
			MaxInputChars: v.GetInt("gateway.openai.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.openai.timeout"), 5*time.Minute),
		}, gateway.ProviderConfig{
			Adapter: "anthropic", BaseURL: v.GetString("gateway.anthropic.base_url"),
			APIKey: anthropicKey, Model: v.GetString("gateway.anthropic.model"),
			MaxInputChars: v.GetInt("gateway.anthropic.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.anthropic.timeout"), 5*time.Minute),
		}, gateway.ProviderConfig{
			Adapter: "ollama", BaseURL: v.GetString("gateway.ollama.base_url"),
			Model:         v.GetString("gateway.ollama.model"),
			MaxInputChars: v.GetInt("gateway.ollama.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.ollama.timeout"), 5*time.Minute),
		}, gateway.ProviderConfig{
			Adapter: "llamacpp", BaseURL: v.GetString("gateway.llamacpp.base_url"),
			Model:         v.GetString("gateway.llamacpp.model"),
			MaxInputChars: v.GetInt("gateway.llamacpp.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.llamacpp.timeout"), 5*time.Minute),
		}, gateway.ProviderConfig{
			Adapter: "horde", BaseURL: v.GetString("gateway.horde.base_url"),
			APIKey: v.GetString("gateway.horde.api_key"), Model: v.GetString("gateway.horde.model"),
			MaxInputChars: v.GetInt("gateway.horde.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.horde.timeout"), 5*time.Minute),
			Options: map[string]any{
				"poll_interval": hordePollInterval(v),
			},
		}, gateway.ProviderConfig{
			Name: "gemini", Adapter: "openai", BaseURL: v.GetString("gateway.gemini.base_url"),
			APIKey: geminiKey, Model: v.GetString("gateway.gemini.model"),
			MaxInputChars: v.GetInt("gateway.gemini.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.gemini.timeout"), 5*time.Minute),
		}
}

func hordePollInterval(v *viper.Viper) time.Duration {
	poll := durationOrDefault(v.GetDuration("gateway.horde.poll_interval"), 4*time.Second)
	if v.IsSet("gateway.horde.options.poll_interval") {
		poll = durationOrDefault(v.GetDuration("gateway.horde.options.poll_interval"), poll)
	}
	return poll
}

func canonicalGatewayProvider(cfg gateway.ProviderConfig) gateway.ProviderConfig {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Adapter = strings.ToLower(strings.TrimSpace(cfg.Adapter))
	if cfg.Adapter == "" {
		cfg.Adapter = strings.ToLower(cfg.Name)
	}
	if cfg.Name == "" {
		cfg.Name = cfg.Adapter
	}
	if cfg.Adapter == string(gateway.ProviderGemini) {
		cfg.Adapter = string(gateway.ProviderOpenAI)
		if strings.EqualFold(cfg.Name, string(gateway.ProviderGemini)) || cfg.Name == "" {
			cfg.Name = string(gateway.ProviderGemini)
		}
	}
	return cfg
}

func genericGatewayAPIKeyEnv(providerName string) string {
	normalized := strings.ToUpper(strings.ReplaceAll(providerName, "-", "_"))
	return "AGENTD_GATEWAY_" + normalized + "_API_KEY"
}

func loadGatewayProviders(v *viper.Viper, process, dotenv map[string]string) ([]gateway.ProviderConfig, error) {
	return loadGatewayProvidersWithWarnings(v, process, dotenv, true)
}

func loadGatewayProvidersNoWarn(v *viper.Viper, process, dotenv map[string]string) ([]gateway.ProviderConfig, error) {
	return loadGatewayProvidersWithWarnings(v, process, dotenv, false)
}

func loadGatewayProvidersWithWarnings(
	v *viper.Viper,
	process, dotenv map[string]string,
	emitMissingAPIKeyWarn bool,
) ([]gateway.ProviderConfig, error) {
	if !v.IsSet("gateway.providers") {
		return nil, nil
	}
	var providers []gateway.ProviderConfig
	if err := v.UnmarshalKey("gateway.providers", &providers); err != nil {
		return nil, fmt.Errorf("unmarshal gateway.providers: %w", err)
	}
	for i := range providers {
		// Normalize name first so the generic env var pattern uses the resolved name.
		if providers[i].Name == "" {
			providers[i].Name = providers[i].Adapter
		}
		// Env beats inline api_key in the file (same precedence as flat gateway.* keys).
		for _, genericEnv := range providerAPIKeyEnvCandidates(providers[i]) {
			if key := envLookup(process, dotenv, genericEnv); key != "" {
				providers[i].APIKey = key
				break
			}
		}

		if emitMissingAPIKeyWarn && providers[i].APIKey == "" && healthModeFor(providers[i]) == healthModeAPIKey {
			envHint := ""
			if keys := providerAPIKeyEnvCandidates(providers[i]); len(keys) > 0 {
				envHint = keys[0]
			}
			slog.Warn("provider has no API key; set it via config or env",
				"provider", providers[i].Name,
				"env_var", envHint,
			)
		}
	}
	return providers, nil
}

func putGatewayProvider(byName map[string]gateway.ProviderConfig, cfg gateway.ProviderConfig) error {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Adapter = strings.TrimSpace(cfg.Adapter)
	if cfg.Name == "" {
		cfg.Name = cfg.Adapter
	}
	if cfg.Name == "" {
		return fmt.Errorf("entry missing name and adapter")
	}
	if cfg.Adapter == "" {
		cfg.Adapter = cfg.Name
	}
	cfg = canonicalGatewayProvider(cfg)
	if _, exists := byName[cfg.Name]; exists {
		return fmt.Errorf("duplicate provider %q", cfg.Name)
	}
	byName[cfg.Name] = cfg
	return nil
}

func (c GatewayConfig) gatewayProvidersByName() (map[string]gateway.ProviderConfig, error) {
	legacyProviders := []struct {
		name string
		cfg  gateway.ProviderConfig
	}{
		{name: "openai", cfg: c.OpenAI},
		{name: "anthropic", cfg: c.Anthropic},
		{name: "ollama", cfg: c.Ollama},
		{name: "llamacpp", cfg: c.LlamaCpp},
		{name: "horde", cfg: c.Horde},
		{name: "gemini", cfg: c.Gemini},
	}
	byName := make(map[string]gateway.ProviderConfig, len(c.Providers)+len(legacyProviders))
	for i, cfg := range c.Providers {
		if err := putGatewayProvider(byName, cfg); err != nil {
			return nil, fmt.Errorf("gateway.providers[%d]: %w", i, err)
		}
	}
	for _, legacy := range legacyProviders {
		if _, exists := byName[legacy.name]; exists {
			continue
		}
		legacy.cfg.Name = legacy.name
		if err := putGatewayProvider(byName, legacy.cfg); err != nil {
			return nil, err
		}
	}
	return byName, nil
}
