package config

import "github.com/spf13/viper"

// SplitConfig caps how far a task is broken into subtasks: by the healing
// split, and by the legacy engine's too_complex reply until T-055b removes it.
type SplitConfig struct {
	// MaxDepth is how many parent→child split generations are allowed before
	// a HUMAN handoff (0 = no cap).
	MaxDepth int

	// MaxSubtasks caps the subtasks created from one split (0 = no cap).
	MaxSubtasks int
}

const (
	DefaultSplitMaxDepth    = 2
	DefaultSplitMaxSubtasks = 5
)

func setSplitDefaults(v *viper.Viper) {
	v.SetDefault("queue.split.max_depth", DefaultSplitMaxDepth)
	v.SetDefault("queue.split.max_subtasks", DefaultSplitMaxSubtasks)
}

func loadSplitConfig(v *viper.Viper) SplitConfig {
	return SplitConfig{
		MaxDepth:    v.GetInt("queue.split.max_depth"),
		MaxSubtasks: v.GetInt("queue.split.max_subtasks"),
	}
}
