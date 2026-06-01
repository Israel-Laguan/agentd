package config

import (
	"time"

	"github.com/spf13/viper"
)

const defaultAPIAddress = "127.0.0.1:8765"

type APIConfig struct {
	Address string
	// MaterializeToken, when non-empty, requires clients to send the same value
	// in header X-Agentd-Materialize-Token on POST /api/v1/projects/materialize.
	MaterializeToken string
}

func setAPIDefaults(v *viper.Viper) {
	v.SetDefault("api.address", defaultAPIAddress)
	v.SetDefault("api.materialize_token", "")
}

func loadAPIConfig(v *viper.Viper) APIConfig {
	return APIConfig{
		Address:          v.GetString("api.address"),
		MaterializeToken: v.GetString("api.materialize_token"),
	}
}

const defaultBreakerHandoffAfter = 2 * time.Minute

type BreakerConfig struct {
	HandoffAfter time.Duration
}

func setBreakerDefaults(v *viper.Viper) {
	v.SetDefault("breaker.handoff_after", defaultBreakerHandoffAfter.String())
}

func loadBreakerConfig(v *viper.Viper) BreakerConfig {
	return BreakerConfig{HandoffAfter: v.GetDuration("breaker.handoff_after")}
}

const defaultDiskFreeThresholdPercent = 10.0

type DiskConfig struct {
	FreeThresholdPercent float64
}

func setDiskDefaults(v *viper.Viper) {
	v.SetDefault("disk.free_threshold_percent", defaultDiskFreeThresholdPercent)
}

func loadDiskConfig(v *viper.Viper) DiskConfig {
	return DiskConfig{FreeThresholdPercent: v.GetFloat64("disk.free_threshold_percent")}
}

const defaultHeartbeatStaleAfter = 2 * time.Minute

type HeartbeatConfig struct {
	StaleAfter time.Duration
}

func setHeartbeatDefaults(v *viper.Viper) {
	v.SetDefault("heartbeat.stale_after", defaultHeartbeatStaleAfter.String())
}

func loadHeartbeatConfig(v *viper.Viper) HeartbeatConfig {
	return HeartbeatConfig{StaleAfter: v.GetDuration("heartbeat.stale_after")}
}

const (
	HealingStrategyIncreaseEffort    = "increase_effort"
	HealingStrategyMinimizeVariables = "minimize_variables"
)

type HealingConfig struct {
	Enabled              bool
	Strategy             string
	Steps                []string
	MaxAdjustments       int
	UpgradeModel         string
	UpgradeProvider      string
	ContextMultiplier    float64
	MaxHealingTasks      int
	OutageHandoffEnabled bool
}

func setHealingDefaults(v *viper.Viper) {
	v.SetDefault("healing.enabled", true)
	v.SetDefault("healing.strategy", HealingStrategyIncreaseEffort)
	v.SetDefault("healing.steps", []string{})
	v.SetDefault("healing.max_adjustments", 0)
	v.SetDefault("healing.upgrade_model", "")
	v.SetDefault("healing.upgrade_provider", "")
	v.SetDefault("healing.context_multiplier", 2.0)
	v.SetDefault("healing.max_healing_tasks", 0)
	v.SetDefault("healing.outage_handoff_enabled", true)
}

func loadHealingConfig(v *viper.Viper) HealingConfig {
	return HealingConfig{
		Enabled:              v.GetBool("healing.enabled"),
		Strategy:             v.GetString("healing.strategy"),
		Steps:                v.GetStringSlice("healing.steps"),
		MaxAdjustments:       v.GetInt("healing.max_adjustments"),
		UpgradeModel:         v.GetString("healing.upgrade_model"),
		UpgradeProvider:      v.GetString("healing.upgrade_provider"),
		ContextMultiplier:    v.GetFloat64("healing.context_multiplier"),
		MaxHealingTasks:      v.GetInt("healing.max_healing_tasks"),
		OutageHandoffEnabled: v.GetBool("healing.outage_handoff_enabled"),
	}
}
