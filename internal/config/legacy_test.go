package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestLegacyConfig_Defaults(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setLegacyDefaults(v)
	cfg := loadLegacyConfig(v)
	if cfg.MaxBreakdownDepth != DefaultLegacyMaxBreakdownDepth {
		t.Errorf("MaxBreakdownDepth = %d, want %d", cfg.MaxBreakdownDepth, DefaultLegacyMaxBreakdownDepth)
	}
	if cfg.MaxSubtasksPerBreakdown != DefaultLegacyMaxSubtasksPerBreakdown {
		t.Errorf("MaxSubtasksPerBreakdown = %d, want %d", cfg.MaxSubtasksPerBreakdown, DefaultLegacyMaxSubtasksPerBreakdown)
	}
	if cfg.PreflightScore != DefaultLegacyPreflightScore {
		t.Errorf("PreflightScore = %d, want %d", cfg.PreflightScore, DefaultLegacyPreflightScore)
	}
}
