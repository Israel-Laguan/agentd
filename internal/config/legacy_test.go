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
	if cfg.PreflightScore != DefaultLegacyPreflightScore {
		t.Errorf("PreflightScore = %d, want %d", cfg.PreflightScore, DefaultLegacyPreflightScore)
	}
	if cfg.RejectScore != DefaultLegacyRejectScore {
		t.Errorf("RejectScore = %d, want %d", cfg.RejectScore, DefaultLegacyRejectScore)
	}
	if cfg.MaxDescriptionLen != DefaultLegacyMaxDescriptionLen {
		t.Errorf("MaxDescriptionLen = %d, want %d", cfg.MaxDescriptionLen, DefaultLegacyMaxDescriptionLen)
	}
}
