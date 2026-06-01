package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestAPIConfig_Defaults(t *testing.T) {
	cfg := APIConfig{
		Address:          defaultAPIAddress,
		MaterializeToken: "",
	}
	if cfg.Address != defaultAPIAddress {
		t.Errorf("Address = %v, want %v", cfg.Address, defaultAPIAddress)
	}
	if cfg.MaterializeToken != "" {
		t.Errorf("MaterializeToken = %v, want empty", cfg.MaterializeToken)
	}
}

func TestAPIConfig_Custom(t *testing.T) {
	cfg := APIConfig{
		Address:          "0.0.0.0:9000",
		MaterializeToken: "secret-token",
	}
	if cfg.Address != "0.0.0.0:9000" {
		t.Errorf("Address = %v, want 0.0.0.0:9000", cfg.Address)
	}
	if cfg.MaterializeToken != "secret-token" {
		t.Errorf("MaterializeToken = %v, want secret-token", cfg.MaterializeToken)
	}
}

func TestDefaultAPIAddress(t *testing.T) {
	if defaultAPIAddress != "127.0.0.1:8765" {
		t.Errorf("defaultAPIAddress = %v, want 127.0.0.1:8765", defaultAPIAddress)
	}
}

func TestBreakerConfig_Defaults(t *testing.T) {
	cfg := BreakerConfig{
		HandoffAfter: defaultBreakerHandoffAfter,
	}
	if cfg.HandoffAfter != 2*time.Minute {
		t.Errorf("HandoffAfter = %v, want 2m", cfg.HandoffAfter)
	}
}

func TestBreakerConfig_Custom(t *testing.T) {
	cfg := BreakerConfig{
		HandoffAfter: 5 * time.Minute,
	}
	if cfg.HandoffAfter != 5*time.Minute {
		t.Errorf("HandoffAfter = %v, want 5m", cfg.HandoffAfter)
	}
}

func TestDiskConfig(t *testing.T) {
	v := viper.New()
	setDiskDefaults(v)

	if got := v.GetFloat64("disk.free_threshold_percent"); got != defaultDiskFreeThresholdPercent {
		t.Errorf("expected default %v, got %v", defaultDiskFreeThresholdPercent, got)
	}

	cfg := loadDiskConfig(v)
	if cfg.FreeThresholdPercent != defaultDiskFreeThresholdPercent {
		t.Errorf("expected %v, got %v", defaultDiskFreeThresholdPercent, cfg.FreeThresholdPercent)
	}

	v.Set("disk.free_threshold_percent", 20.0)
	cfg = loadDiskConfig(v)
	if cfg.FreeThresholdPercent != 20.0 {
		t.Errorf("expected 20.0, got %v", cfg.FreeThresholdPercent)
	}
}

func TestHeartbeatConfig_Defaults(t *testing.T) {
	cfg := HeartbeatConfig{
		StaleAfter: defaultHeartbeatStaleAfter,
	}
	if cfg.StaleAfter != 2*time.Minute {
		t.Errorf("StaleAfter = %v, want 2m", cfg.StaleAfter)
	}
}

func TestHeartbeatConfig_Custom(t *testing.T) {
	cfg := HeartbeatConfig{
		StaleAfter: 5 * time.Minute,
	}
	if cfg.StaleAfter != 5*time.Minute {
		t.Errorf("StaleAfter = %v, want 5m", cfg.StaleAfter)
	}
}

func TestHealingConfig_Defaults(t *testing.T) {
	cfg := HealingConfig{
		Enabled:           true,
		Strategy:          HealingStrategyIncreaseEffort,
		Steps:             []string{},
		MaxAdjustments:    0,
		UpgradeModel:      "",
		UpgradeProvider:   "",
		ContextMultiplier: 2.0,
	}
	if !cfg.Enabled {
		t.Error("Enabled should be true by default")
	}
	if cfg.Strategy != HealingStrategyIncreaseEffort {
		t.Errorf("Strategy = %v, want increase_effort", cfg.Strategy)
	}
	if cfg.ContextMultiplier != 2.0 {
		t.Errorf("ContextMultiplier = %v, want 2.0", cfg.ContextMultiplier)
	}
}

func TestHealingConfig_Custom(t *testing.T) {
	cfg := HealingConfig{
		Enabled:           false,
		Strategy:          HealingStrategyMinimizeVariables,
		Steps:             []string{"retry", "upgrade"},
		MaxAdjustments:    3,
		UpgradeModel:      "gpt-4",
		UpgradeProvider:   "openai",
		ContextMultiplier: 3.0,
	}
	if cfg.Enabled {
		t.Error("Enabled should be false")
	}
	if cfg.Strategy != HealingStrategyMinimizeVariables {
		t.Errorf("Strategy = %v, want minimize_variables", cfg.Strategy)
	}
	if len(cfg.Steps) != 2 {
		t.Errorf("Steps length = %v, want 2", len(cfg.Steps))
	}
	if cfg.MaxAdjustments != 3 {
		t.Errorf("MaxAdjustments = %v, want 3", cfg.MaxAdjustments)
	}
	if cfg.UpgradeModel != "gpt-4" {
		t.Errorf("UpgradeModel = %v, want gpt-4", cfg.UpgradeModel)
	}
}

func TestHealingStrategy_Constants(t *testing.T) {
	if HealingStrategyIncreaseEffort != "increase_effort" {
		t.Errorf("HealingStrategyIncreaseEffort = %v", HealingStrategyIncreaseEffort)
	}
	if HealingStrategyMinimizeVariables != "minimize_variables" {
		t.Errorf("HealingStrategyMinimizeVariables = %v", HealingStrategyMinimizeVariables)
	}
}

func TestHealingConfig_MaxHealingTasks(t *testing.T) {
	cfg := HealingConfig{
		Enabled:         true,
		MaxHealingTasks: 5,
	}
	if cfg.MaxHealingTasks != 5 {
		t.Errorf("MaxHealingTasks = %v, want 5", cfg.MaxHealingTasks)
	}
}

func TestHealingConfig_MaxHealingTasksDefaultZero(t *testing.T) {
	cfg := HealingConfig{
		Enabled: true,
	}
	if cfg.MaxHealingTasks != 0 {
		t.Errorf("MaxHealingTasks = %v, want 0 (no cap)", cfg.MaxHealingTasks)
	}
}
