package main

import (
	"testing"

	"agentd/internal/config"
	"agentd/internal/queue"
)

func TestBuildWorkerOptions_PassesAgenticConfig(t *testing.T) {
	tests := []struct {
		name  string
		set   func(*config.Config)
		check func(*testing.T, queue.WorkerOptions)
	}{
		{
			name: "batching",
			set: func(cfg *config.Config) {
				cfg.Agentic.Batching = config.BatchingConfig{Enabled: true, MaxBatchSize: 3}
			},
			check: func(t *testing.T, opts queue.WorkerOptions) {
				if !opts.Batching.Enabled {
					t.Fatal("WorkerOptions.Batching.Enabled = false, want true")
				}
				if opts.Batching.MaxBatchSize != 3 {
					t.Fatalf("WorkerOptions.Batching.MaxBatchSize = %d, want 3", opts.Batching.MaxBatchSize)
				}
			},
		},
		{
			name: "tool_manifest",
			set: func(cfg *config.Config) {
				cfg.Agentic.ToolManifest = config.ToolManifestConfig{Enabled: true, MinConfidence: 0.7}
			},
			check: func(t *testing.T, opts queue.WorkerOptions) {
				if !opts.ToolManifest.Enabled {
					t.Fatal("WorkerOptions.ToolManifest.Enabled = false, want true")
				}
				if opts.ToolManifest.MinConfidence != 0.7 {
					t.Fatalf("WorkerOptions.ToolManifest.MinConfidence = %v, want 0.7", opts.ToolManifest.MinConfidence)
				}
			},
		},
		{
			name: "capability_routing",
			set: func(cfg *config.Config) {
				cfg.Agentic.CapabilityRouting = config.CapabilityRoutingConfig{Enabled: true, Mappings: map[string]string{"generate_image": "dalle"}}
			},
			check: func(t *testing.T, opts queue.WorkerOptions) {
				if !opts.CapabilityRouting.Enabled {
					t.Fatal("WorkerOptions.CapabilityRouting.Enabled = false, want true")
				}
				if got := opts.CapabilityRouting.Mappings["generate_image"]; got != "dalle" {
					t.Fatalf("WorkerOptions.CapabilityRouting.Mappings[generate_image] = %q, want dalle", got)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{}
			tt.set(&cfg)

			opts := buildWorkerOptions(nil, runtimeDeps{}, cfg, "", nil)

			tt.check(t, opts)
		})
	}
}
