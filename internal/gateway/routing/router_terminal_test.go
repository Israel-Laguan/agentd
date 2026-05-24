package routing

import (
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func TestDecideTerminalError(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("provider down")

	tests := []struct {
		name                   string
		req                    spec.AIRequest
		matchedProvider        bool
		selectedHasToolSupport bool
		providerErrs           []error
		wantSubstr             string
		wantLLMUnreachable     bool
	}{
		{
			name: "explicit provider missing tools support",
			req: spec.AIRequest{
				Provider: "ollama",
				Tools:    []spec.ToolDefinition{{Name: "test_tool"}},
			},
			matchedProvider:        true,
			selectedHasToolSupport: false,
			wantSubstr:             "does not support tools",
		},
		{
			name: "explicit provider not configured",
			req:  spec.AIRequest{Provider: "openai"},
			wantSubstr: "not configured",
		},
		{
			name:               "whitespace-only provider treated as unspecified",
			req:                spec.AIRequest{Provider: "   "},
			wantSubstr:         "all providers skipped",
			wantLLMUnreachable: true,
		},
		{
			name:               "all providers skipped",
			wantSubstr:         "all providers skipped",
			wantLLMUnreachable: true,
		},
		{
			name:               "cascade failures",
			providerErrs:       []error{providerErr},
			wantSubstr:         "provider down",
			wantLLMUnreachable: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := decideTerminalError(tt.req, tt.matchedProvider, tt.selectedHasToolSupport, tt.providerErrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantSubstr)
			}
			if tt.wantLLMUnreachable && !errors.Is(err, models.ErrLLMUnreachable) {
				t.Fatalf("error = %v, want ErrLLMUnreachable", err)
			}
		})
	}
}

func TestErrTruncation(t *testing.T) {
	t.Parallel()

	base := errors.New("budget exceeded")
	wrapped := errTruncation{err: base}
	if wrapped.Error() != base.Error() {
		t.Fatalf("Error() = %q, want %q", wrapped.Error(), base.Error())
	}
	if !errors.Is(wrapped, base) {
		t.Fatal("Unwrap() did not preserve base error")
	}
}
