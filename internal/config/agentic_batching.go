package config

import "github.com/spf13/viper"

const DefaultBatchingMaxBatchSize = 5

// BatchingConfig controls multi-task LLM request consolidation in the worker.
type BatchingConfig struct {
	Enabled      bool
	MaxBatchSize int
}

func setBatchingDefaults(v *viper.Viper) {
	v.SetDefault("agentic.batching.enabled", false)
	v.SetDefault("agentic.batching.max_batch_size", DefaultBatchingMaxBatchSize)
}

func loadBatchingConfig(v *viper.Viper) BatchingConfig {
	max := v.GetInt("agentic.batching.max_batch_size")
	if max < 1 {
		max = DefaultBatchingMaxBatchSize
	}
	return BatchingConfig{
		Enabled:      v.GetBool("agentic.batching.enabled"),
		MaxBatchSize: max,
	}
}
