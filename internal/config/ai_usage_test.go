package config

import (
	"strings"
	"testing"
)

func TestLoadAIUsageRetentionDefaultsToKeepForever(t *testing.T) {
	cfg, err := loadWithYAML(t, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AIUsage.RetentionDays != 0 {
		t.Fatalf("RetentionDays = %d, want 0 (keep forever)", cfg.AIUsage.RetentionDays)
	}
}

func TestLoadAIUsageRetentionReadsDays(t *testing.T) {
	cfg, err := loadWithYAML(t, "ai_usage:\n  retention_days: 14\n")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AIUsage.RetentionDays != 14 {
		t.Fatalf("RetentionDays = %d, want 14", cfg.AIUsage.RetentionDays)
	}
}

func TestLoadRejectsNegativeAIUsageRetention(t *testing.T) {
	_, err := loadWithYAML(t, "ai_usage:\n  retention_days: -1\n")
	if err == nil || !strings.Contains(err.Error(), "ai_usage.retention_days") {
		t.Fatalf("Load() error = %v, want an ai_usage.retention_days error", err)
	}
}
