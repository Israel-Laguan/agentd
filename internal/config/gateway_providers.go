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
			Type: "openai", BaseURL: v.GetString("gateway.openai.base_url"),
			APIKey: openAIKey, Model: v.GetString("gateway.openai.model"),
			MaxInputChars: v.GetInt("gateway.openai.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.openai.timeout"), 5*time.Minute),
		}, gateway.ProviderConfig{
			Type: "anthropic", BaseURL: v.GetString("gateway.anthropic.base_url"),
			APIKey: anthropicKey, Model: v.GetString("gateway.anthropic.model"),
			MaxInputChars: v.GetInt("gateway.anthropic.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.anthropic.timeout"), 5*time.Minute),
		}, gateway.ProviderConfig{
			Type: "ollama", BaseURL: v.GetString("gateway.ollama.base_url"),
			Model:         v.GetString("gateway.ollama.model"),
			MaxInputChars: v.GetInt("gateway.ollama.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.ollama.timeout"), 5*time.Minute),
		}, gateway.ProviderConfig{
			Type: "llamacpp", BaseURL: v.GetString("gateway.llamacpp.base_url"),
			Model:         v.GetString("gateway.llamacpp.model"),
			MaxInputChars: v.GetInt("gateway.llamacpp.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.llamacpp.timeout"), 5*time.Minute),
		}, gateway.ProviderConfig{
			Type: "horde", BaseURL: v.GetString("gateway.horde.base_url"),
			APIKey: v.GetString("gateway.horde.api_key"), Model: v.GetString("gateway.horde.model"),
			MaxInputChars: v.GetInt("gateway.horde.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.horde.timeout"), 5*time.Minute),
			PollInterval:  durationOrDefault(v.GetDuration("gateway.horde.poll_interval"), 4*time.Second),
		}, gateway.ProviderConfig{
			Type: "gemini", BaseURL: v.GetString("gateway.gemini.base_url"),
			APIKey: geminiKey, Model: v.GetString("gateway.gemini.model"),
			MaxInputChars: v.GetInt("gateway.gemini.max_input_chars"),
			Timeout:       durationOrDefault(v.GetDuration("gateway.gemini.timeout"), 5*time.Minute),
		}
}

func loadGatewayProviders(v *viper.Viper, process, dotenv map[string]string) ([]gateway.ProviderConfig, error) {
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
			providers[i].Name = providers[i].Type
		}
		if providers[i].APIKey == "" && providers[i].APIKeyEnv != "" {
			providers[i].APIKey = envLookup(process, dotenv, providers[i].APIKeyEnv)
		}
		if providers[i].APIKey == "" {
			generic := "AGENTD_GATEWAY_" + strings.ToUpper(providers[i].Name) + "_API_KEY"
			providers[i].APIKey = envLookup(process, dotenv, generic)
		}
	}
	for _, p := range providers {
		if p.APIKey == "" && healthModeFor(p) == healthModeAPIKey {
			envHint := "AGENTD_GATEWAY_" + strings.ToUpper(p.Name) + "_API_KEY"
			if p.APIKeyEnv != "" {
				envHint = p.APIKeyEnv
			}
			slog.Warn("provider has no API key; set it via config or env",
				"provider", p.Name,
				"env_var", envHint,
			)
		}
	}
	return providers, nil
}
