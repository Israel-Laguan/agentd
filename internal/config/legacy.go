package config

import "github.com/spf13/viper"

// LegacyConfig controls non-agentic JSON-command worker behavior.
type LegacyConfig struct {
	// PreflightScore adds a user-message hint when ComplexityScorer meets this threshold (0 = off).
	PreflightScore int

	// RejectScore skips the LLM call and handoffs immediately when score meets threshold (0 = off).
	RejectScore int

	// MaxDescriptionLen skips the LLM when task description exceeds this length (0 = off).
	MaxDescriptionLen int
}

const (
	DefaultLegacyPreflightScore    = 6
	DefaultLegacyRejectScore       = 9
	DefaultLegacyMaxDescriptionLen = 2000
)

func setLegacyDefaults(v *viper.Viper) {
	v.SetDefault("queue.legacy.preflight_score", DefaultLegacyPreflightScore)
	v.SetDefault("queue.legacy.reject_score", DefaultLegacyRejectScore)
	v.SetDefault("queue.legacy.max_description_len", DefaultLegacyMaxDescriptionLen)
}

func loadLegacyConfig(v *viper.Viper) LegacyConfig {
	return LegacyConfig{
		PreflightScore:    v.GetInt("queue.legacy.preflight_score"),
		RejectScore:       v.GetInt("queue.legacy.reject_score"),
		MaxDescriptionLen: v.GetInt("queue.legacy.max_description_len"),
	}
}
