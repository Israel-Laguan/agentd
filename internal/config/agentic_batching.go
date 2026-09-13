package config

const (
	DefaultBatchingMaxBatchSize = 5
	MaxBatchingMaxBatchSize     = 32
)

	// BatchingConfig controls multi-task LLM request consolidation in the worker.
type BatchingConfig struct {
	Enabled      bool
	MaxBatchSize int
}

