package config

import "github.com/spf13/viper"

const (
	DefaultBatchingMaxBatchSize = 5
	MaxBatchingMaxBatchSize     = 32
)

// BatchingConfig controls multi-task LLM request consolidation in the worker.
type BatchingConfig struct {
	Enabled      bool
	MaxBatchSize int
}

func loadBatchingConfig(v *viper.Viper) BatchingConfig {
	batchMax := v.GetInt("agentic.batching.max_batch_size")
	if batchMax < 1 {
		batchMax = DefaultBatchingMaxBatchSize
	}
	if batchMax > MaxBatchingMaxBatchSize {
		batchMax = MaxBatchingMaxBatchSize
	}
	return BatchingConfig{
		Enabled:      v.GetBool("agentic.batching.enabled"),
		MaxBatchSize: batchMax,
	}
}
