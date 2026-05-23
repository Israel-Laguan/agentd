package main

import (
	"errors"
	"fmt"
	"testing"

	"agentd/internal/config"
)

func TestDescribeCommandError_Sentinels(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantSummary string
	}{
		{
			name:        "config read",
			err:         fmt.Errorf("load configuration: %w", fmt.Errorf("%w: read config: boom", config.ErrConfigRead)),
			wantSummary: "agentd could not read its configuration file.",
		},
		{
			name:        "dirs not writable",
			err:         fmt.Errorf("%w /tmp: permission denied", config.ErrDirsNotWritable),
			wantSummary: "agentd could not write to one of its data directories.",
		},
		{
			name:        "llm warmup",
			err:         fmt.Errorf("%w: timeout", config.ErrLLMWarmup),
			wantSummary: "agentd reached your LLM provider, but the startup warmup failed.",
		},
		{
			name:        "no providers",
			err:         fmt.Errorf("%w: configure keys", config.ErrNoLLMProviders),
			wantSummary: "agentd could not find a usable LLM provider.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, hint := describeCommandError(tt.err)
			if summary != tt.wantSummary {
				t.Fatalf("summary = %q, want %q", summary, tt.wantSummary)
			}
			if hint == "" {
				t.Fatal("hint is empty, want non-empty guidance")
			}
		})
	}
}

func TestDescribeCommandError_Nil(t *testing.T) {
	summary, hint := describeCommandError(nil)
	if summary != "" || hint != "" {
		t.Fatalf("describeCommandError(nil) = (%q, %q), want empty", summary, hint)
	}
}

func TestRequireStartupProviders_ErrNoLLMProviders(t *testing.T) {
	t.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	err := requireStartupProviders(config.GatewayConfig{Order: []string{"openai"}})
	if err == nil {
		t.Fatal("requireStartupProviders() error = nil, want ErrNoLLMProviders")
	}
	if !errors.Is(err, config.ErrNoLLMProviders) {
		t.Fatalf("error = %v, want %v", err, config.ErrNoLLMProviders)
	}
}
