package main

import (
	"testing"

	"agentd/internal/config"
	"agentd/internal/queue"
)

var agenticConfigCases = []struct {
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
			cfg.Agentic.CapabilityRouting = config.CapabilityRoutingConfig{
				Enabled:       true,
				MinConfidence: 0.7,
				Mappings:      map[string]string{"generate_image": "dalle"},
				Tools:         map[string]string{"generate_image": "generate"},
			}
		},
		check: func(t *testing.T, opts queue.WorkerOptions) {
			if !opts.CapabilityRouting.Enabled {
				t.Fatal("WorkerOptions.CapabilityRouting.Enabled = false, want true")
			}
			if opts.CapabilityRouting.MinConfidence != 0.7 {
				t.Fatalf("WorkerOptions.CapabilityRouting.MinConfidence = %v, want 0.7", opts.CapabilityRouting.MinConfidence)
			}
			if got := opts.CapabilityRouting.Mappings["generate_image"]; got != "dalle" {
				t.Fatalf("WorkerOptions.CapabilityRouting.Mappings[generate_image] = %q, want dalle", got)
			}
			if got := opts.CapabilityRouting.Tools["generate_image"]; got != "generate" {
				t.Fatalf("WorkerOptions.CapabilityRouting.Tools[generate_image] = %q, want generate", got)
			}
		},
	},
}

func TestBuildWorkerOptions_PassesAgenticConfig(t *testing.T) {
	for _, tt := range agenticConfigCases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{}
			tt.set(&cfg)

			opts := buildWorkerOptions(nil, runtimeDeps{}, cfg, "", nil)

			tt.check(t, opts)
		})
	}
}
