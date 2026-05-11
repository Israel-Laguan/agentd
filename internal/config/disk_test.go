package config

import (
	"testing"

	"github.com/spf13/viper"
)

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