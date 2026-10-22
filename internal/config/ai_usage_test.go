package config

import (
	"strconv"
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

// "14d" read through GetInt became 0, passed the range check, and kept every
// row forever: a typo turned retention off silently.
func TestLoadRejectsANonNumericAIUsageRetention(t *testing.T) {
	for _, value := range []string{"14d", "two weeks", "1.5"} {
		_, err := loadWithYAML(t, "ai_usage:\n  retention_days: "+strconv.Quote(value)+"\n")
		if err == nil || !strings.Contains(err.Error(), "ai_usage.retention_days") {
			t.Errorf("Load(%q) error = %v, want an ai_usage.retention_days error", value, err)
		}
	}
}
