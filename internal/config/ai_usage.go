package config

import "fmt"

// AIUsageConfig controls how long the per-call usage records (ai_calls and
// tool_calls, see docs/ai-usage-report.md) are kept (T-056).
type AIUsageConfig struct {
	// RetentionDays deletes records older than this many days. 0 keeps them forever.
	RetentionDays int
}

// validateAIUsageConfig rejects a negative retention: a negative cut would
// delete rows written after the daemon started.
func validateAIUsageConfig(cfg AIUsageConfig) error {
	if cfg.RetentionDays < 0 {
		return fmt.Errorf("ai_usage.retention_days must be 0 (keep forever) or a positive number of days, got %d", cfg.RetentionDays)
	}
	return nil
}
