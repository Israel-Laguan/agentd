package config

import (
	"testing"
)

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