package main

import (
	"testing"

	"agentd/internal/config"
)

func TestBuildWorkerOptions_PassesAgenticBatchingConfig(t *testing.T) {
	cfg := config.Config{}
	cfg.Agentic.Batching = config.BatchingConfig{Enabled: true, MaxBatchSize: 3}

	opts := buildWorkerOptions(nil, runtimeDeps{}, cfg, "", nil)

	if !opts.Batching.Enabled {
		t.Fatal("WorkerOptions.Batching.Enabled = false, want true")
	}
	if opts.Batching.MaxBatchSize != 3 {
		t.Fatalf("WorkerOptions.Batching.MaxBatchSize = %d, want 3", opts.Batching.MaxBatchSize)
	}
}
