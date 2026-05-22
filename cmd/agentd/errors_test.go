package main

import (
	"fmt"
	"testing"
)

func TestDescribeCommandError_LLMWarmup(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantHint string
	}{
		{
			name:     "openai warmup failed",
			err:      fmt.Errorf("LLM warmup failed" + " (provider=openai): timeout"),
			wantHint: "Check the provider order, API key, model name, and network access.",
		},
		{
			name:     "horde warmup wrapped",
			err:      fmt.Errorf("LLM warmup: horde warmup failed: connection refused"),
			wantHint: "Check the provider order, API key, model name, and network access.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, hint := describeCommandError(tt.err)
			if hint != tt.wantHint {
				t.Errorf("hint = %q, want %q", hint, tt.wantHint)
			}
		})
	}
}
