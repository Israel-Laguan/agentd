package config

import "github.com/spf13/viper"

// hydrateChecked loads the sections whose values are validated as they load.
func hydrateChecked(cfg *Config, v *viper.Viper) error {
	cfg.Sandbox = loadSandboxConfig(v)
	if err := validateSandboxConfig(cfg.Sandbox); err != nil {
		return err
	}
	var err error
	if cfg.AIUsage, err = loadAIUsageConfig(v); err != nil {
		return err
	}
	return validateAIUsageConfig(cfg.AIUsage)
}
