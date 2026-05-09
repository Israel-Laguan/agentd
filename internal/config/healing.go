package config

import "github.com/spf13/viper"

const (
	HealingStrategyIncreaseEffort    = "increase_effort"
	HealingStrategyMinimizeVariables = "minimize_variables"
)

type HealingConfig struct {
	Enabled           bool
	Strategy          string
	Steps             []string
	MaxAdjustments    int
	UpgradeModel      string
	UpgradeProvider   string
	ContextMultiplier float64
}

func setHealingDefaults(v *viper.Viper) {
	v.SetDefault("healing.enabled", true)
	v.SetDefault("healing.strategy", HealingStrategyIncreaseEffort)
	v.SetDefault("healing.steps", []string{})
	v.SetDefault("healing.max_adjustments", 0)
	v.SetDefault("healing.upgrade_model", "")
	v.SetDefault("healing.upgrade_provider", "")
	v.SetDefault("healing.context_multiplier", 2.0)
}

func loadHealingConfig(v *viper.Viper) HealingConfig {
	return HealingConfig{
		Enabled:           v.GetBool("healing.enabled"),
		Strategy:          v.GetString("healing.strategy"),
		Steps:             v.GetStringSlice("healing.steps"),
		MaxAdjustments:    v.GetInt("healing.max_adjustments"),
		UpgradeModel:      v.GetString("healing.upgrade_model"),
		UpgradeProvider:   v.GetString("healing.upgrade_provider"),
		ContextMultiplier: v.GetFloat64("healing.context_multiplier"),
	}
}
