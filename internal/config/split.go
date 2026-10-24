package config

import (
	"fmt"

	"github.com/spf13/viper"
)

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

// validateSplitConfig rejects negative caps: the worker reads 0 as "no cap",
// so a negative value would silently remove the limit instead of tightening it.
func validateSplitConfig(cfg SplitConfig) error {
	if cfg.MaxDepth < 0 {
		return fmt.Errorf("queue.split.max_depth must be 0 (no cap) or a positive number, got %d", cfg.MaxDepth)
	}
	if cfg.MaxSubtasks < 0 {
		return fmt.Errorf("queue.split.max_subtasks must be 0 (no cap) or a positive number, got %d", cfg.MaxSubtasks)
	}
	return nil
}
