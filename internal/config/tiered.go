package config

import "github.com/spf13/viper"

const (
	DefaultTieredEnabled            = false
	DefaultTieredComplexityThreshold = 200
)

// TieredModelTarget maps a pipeline step kind to a provider and model.
type TieredModelTarget struct {
	Provider string
	Model    string
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
	// Models maps step kinds to provider/model overrides (v1 — optional).
	Models map[string]TieredModelTarget
}

func setTieredDefaults(v *viper.Viper) {
	v.SetDefault("tiered.enabled", DefaultTieredEnabled)
	v.SetDefault("tiered.complexity_threshold", DefaultTieredComplexityThreshold)
}

func loadTieredConfig(v *viper.Viper) TieredConfig {
	threshold := v.GetInt("tiered.complexity_threshold")
	if threshold < 0 {
		threshold = 0
	}
	models := make(map[string]TieredModelTarget)
	for _, kind := range []string{"context", "decision", "execute", "verify", "escalate"} {
		prefix := "tiered.models." + kind
		provider := v.GetString(prefix + ".provider")
		model := v.GetString(prefix + ".model")
		if provider != "" || model != "" {
			models[kind] = TieredModelTarget{Provider: provider, Model: model}
		}
	}
	return TieredConfig{
		Enabled:             v.GetBool("tiered.enabled"),
		ComplexityThreshold: threshold,
		Models:              models,
	}
}
