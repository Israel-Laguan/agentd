package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// AIUsageConfig controls how long the per-call usage records (ai_calls and
// tool_calls, see docs/ai-usage-report.md) are kept (T-056).
type AIUsageConfig struct {
	// RetentionDays deletes records older than this many days. 0 keeps them forever.
	RetentionDays int
}

// loadAIUsageConfig reads ai_usage.retention_days as a whole number. viper's
// GetInt turns "14d" into 0, which would read as "keep forever"; a value that
// is not a whole number is an error instead.
func loadAIUsageConfig(v *viper.Viper) (AIUsageConfig, error) {
	raw := strings.TrimSpace(v.GetString("ai_usage.retention_days"))
	days, err := strconv.Atoi(raw)
	if err != nil {
		return AIUsageConfig{}, fmt.Errorf("ai_usage.retention_days must be a whole number of days, got %q", raw)
	}
	return AIUsageConfig{RetentionDays: days}, nil
}

// validateAIUsageConfig rejects a negative retention: a negative cut would
// delete rows written after the daemon started.
func validateAIUsageConfig(cfg AIUsageConfig) error {
	if cfg.RetentionDays < 0 {
		return fmt.Errorf("ai_usage.retention_days must be 0 (keep forever) or a positive number of days, got %d", cfg.RetentionDays)
	}
	return nil
}
