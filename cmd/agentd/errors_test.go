package main

import (
	"testing"
)

func TestDescribeCommandError(t *testing.T) {
	tests := []struct {
		name         string
		errText      string
		wantSummary  string
		wantHintPart string
	}{
		{
			name:         "port conflict",
			errText:      "listen tcp 127.0.0.1:8765: bind: address already in use",
			wantSummary:  "Another process is already using the configured API address.",
			wantHintPart: "AGENTD_API_ADDRESS",
		},
		{
			name:         "llm warmup",
			errText:      "LLM warmup failed (provider=gemini): 500 internal server error",
			wantSummary:  "agentd reached your LLM provider, but the startup warmup failed.",
			wantHintPart: "API key",
		},
		{
			name:         "fallback",
			errText:      "something unexpected happened",
			wantSummary:  "agentd could not complete the requested command.",
			wantHintPart: "rerun with --verbose",
		},
		{
			name:         "config load",
			errText:      "load configuration: read config: Unsupported Config Type \"abc\"",
			wantSummary:  "agentd could not read its configuration file.",
			wantHintPart: "AGENTD_* variables",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, hint := describeCommandError(testError(tt.errText))
			if summary != tt.wantSummary {
				t.Fatalf("summary = %q, want %q", summary, tt.wantSummary)
			}
			if tt.wantHintPart != "" && !containsString(hint, tt.wantHintPart) {
				t.Fatalf("hint = %q, want substring %q", hint, tt.wantHintPart)
			}
		})
	}
}

type testError string

func (e testError) Error() string { return string(e) }

func containsString(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}