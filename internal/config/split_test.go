package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestSplitConfig_Defaults(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setSplitDefaults(v)
	cfg := loadSplitConfig(v)
	if cfg.MaxDepth != DefaultSplitMaxDepth || cfg.MaxSubtasks != DefaultSplitMaxSubtasks {
		t.Errorf("SplitConfig = %+v, want depth %d and subtasks %d", cfg, DefaultSplitMaxDepth, DefaultSplitMaxSubtasks)
	}
}

func TestSplitConfig_ReadsQueueSplitKeys(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setSplitDefaults(v)
	v.Set("queue.split.max_depth", 4)
	v.Set("queue.split.max_subtasks", 9)
	cfg := loadSplitConfig(v)
	if cfg.MaxDepth != 4 || cfg.MaxSubtasks != 9 {
		t.Errorf("SplitConfig = %+v, want depth 4 and subtasks 9", cfg)
	}
}

func TestSplitConfig_RejectsNegativeCaps(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		cfg  SplitConfig
	}{
		{name: "max depth", cfg: SplitConfig{MaxDepth: -1, MaxSubtasks: DefaultSplitMaxSubtasks}},
		{name: "max subtasks", cfg: SplitConfig{MaxDepth: DefaultSplitMaxDepth, MaxSubtasks: -1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateSplitConfig(tt.cfg); err == nil {
				t.Fatalf("validateSplitConfig(%+v) = nil, want an error", tt.cfg)
			}
		})
	}
}
