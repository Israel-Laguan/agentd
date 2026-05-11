package config

import (
	"testing"
)

func TestDiskConfig_Defaults(t *testing.T) {
	cfg := DiskConfig{
		FreeThresholdPercent: defaultDiskFreeThresholdPercent,
	}
	if cfg.FreeThresholdPercent != 10.0 {
		t.Errorf("FreeThresholdPercent = %v, want 10.0", cfg.FreeThresholdPercent)
	}
}

func TestDiskConfig_Custom(t *testing.T) {
	cfg := DiskConfig{
		FreeThresholdPercent: 15.0,
	}
	if cfg.FreeThresholdPercent != 15.0 {
		t.Errorf("FreeThresholdPercent = %v, want 15.0", cfg.FreeThresholdPercent)
	}
}