package config

import "github.com/spf13/viper"

const (
	DefaultTieredEnabled             = false
	DefaultTieredComplexityThreshold = 200
	DefaultTieredMaxPaths            = 40
	DefaultTieredMaxChars            = 48000
	// DefaultTieredMaxMidFix caps bounded execute redos after a verify
	// fail/flake before the escalation ladder gives up on mid-fix and
	// escalates to the strong model instead.
	DefaultTieredMaxMidFix = 2
	// DefaultTieredMaxEscalate caps strong-model escalation attempts per
	// pipeline run before handing off to a human.
	DefaultTieredMaxEscalate = 1
	DefaultTieredMaxReGather = 2
)

// TieredContextPackConfig holds budget defaults for ContextPack creation.
type TieredContextPackConfig struct {
	MaxPaths int
	MaxChars int
}

// TieredEscalationConfig holds the escalation ladder's retry caps.
type TieredEscalationConfig struct {
	MaxMidFix   int
	MaxEscalate int
	MaxReGather int
}

// TieredConfig controls the tiered execution pipeline (Phase 5, milestones M1–M2).
// When disabled (the default), the worker always runs one-shot. When enabled,
// only tasks at or above the complexity threshold are split into typed DAG steps.
type TieredConfig struct {
	// Enabled gates the entire tiered pipeline. Default: false.
	Enabled bool
	// ComplexityThreshold is the minimum EstimateTaskComplexity score to split.
	// 0 while enabled disables splitting (same spirit as planning threshold).
	// Unset while enabled defaults to 200.
	ComplexityThreshold int
	// ContextPack holds budget limits for the sealed handoff artifact.
	// These values are wired into NewContextPack and EnforceBudget calls.
	ContextPack TieredContextPackConfig
	// Escalation holds the mid-fix/escalate retry caps for the escalation ladder.
	Escalation TieredEscalationConfig
}

// EscalationConfig returns the escalation ladder config with defaults applied.
func (tc TieredConfig) EscalationConfig() TieredEscalationConfig {
	cfg := tc.Escalation
	if cfg.MaxMidFix <= 0 {
		cfg.MaxMidFix = DefaultTieredMaxMidFix
	}
	if cfg.MaxEscalate <= 0 {
		cfg.MaxEscalate = DefaultTieredMaxEscalate
	}
	if cfg.MaxReGather <= 0 {
		cfg.MaxReGather = DefaultTieredMaxReGather
	}
	return cfg
}

// ContextPackConfig returns the budget config from the tiered config,
// with defaults applied. This is wired into NewContextPack and EnforceBudget.
func (tc TieredConfig) ContextPackConfig() TieredContextPackConfig {
	cfg := tc.ContextPack
	if cfg.MaxPaths <= 0 {
		cfg.MaxPaths = DefaultTieredMaxPaths
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = DefaultTieredMaxChars
	}
	return cfg
}

func setTieredDefaults(v *viper.Viper) {
	v.SetDefault("tiered.enabled", DefaultTieredEnabled)
	v.SetDefault("tiered.complexity_threshold", DefaultTieredComplexityThreshold)
	v.SetDefault("tiered.context_pack.max_paths", DefaultTieredMaxPaths)
	v.SetDefault("tiered.context_pack.max_chars", DefaultTieredMaxChars)
	v.SetDefault("tiered.escalation.max_mid_fix", DefaultTieredMaxMidFix)
	v.SetDefault("tiered.escalation.max_escalate", DefaultTieredMaxEscalate)
	v.SetDefault("tiered.escalation.max_re_gather", DefaultTieredMaxReGather)
}

func loadTieredConfig(v *viper.Viper) TieredConfig {
	threshold := v.GetInt("tiered.complexity_threshold")
	if threshold < 0 {
		threshold = 0
	}
	maxPaths := v.GetInt("tiered.context_pack.max_paths")
	if maxPaths <= 0 {
		maxPaths = DefaultTieredMaxPaths
	}
	maxChars := v.GetInt("tiered.context_pack.max_chars")
	if maxChars <= 0 {
		maxChars = DefaultTieredMaxChars
	}
	maxMidFix := v.GetInt("tiered.escalation.max_mid_fix")
	if maxMidFix <= 0 {
		maxMidFix = DefaultTieredMaxMidFix
	}
	maxEscalate := v.GetInt("tiered.escalation.max_escalate")
	if maxEscalate <= 0 {
		maxEscalate = DefaultTieredMaxEscalate
	}
	maxReGather := v.GetInt("tiered.escalation.max_re_gather")
	if maxReGather <= 0 {
		maxReGather = DefaultTieredMaxReGather
	}
	return TieredConfig{
		Enabled:             v.GetBool("tiered.enabled"),
		ComplexityThreshold: threshold,
		ContextPack: TieredContextPackConfig{
			MaxPaths: maxPaths,
			MaxChars: maxChars,
		},
		Escalation: TieredEscalationConfig{
			MaxMidFix:   maxMidFix,
			MaxEscalate: maxEscalate,
			MaxReGather: maxReGather,
		},
	}
}
