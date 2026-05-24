package routing

import (
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type decideTerminalErrorCase struct {
	name                   string
	req                    spec.AIRequest
	matchedProvider        bool
	selectedHasToolSupport bool
	providerErrs           []error
	wantSubstr             string
	wantLLMUnreachable     bool
}

var decideTerminalErrorCases = []decideTerminalErrorCase{
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
		name:       "explicit provider not configured",
		req:        spec.AIRequest{Provider: "openai"},
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
		providerErrs:       []error{errors.New("provider down")},
		wantSubstr:         "provider down",
		wantLLMUnreachable: true,
	},
}

func TestDecideTerminalError(t *testing.T) {
	t.Parallel()

	for _, tt := range decideTerminalErrorCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertDecideTerminalError(t, tt)
		})
	}
}

func assertDecideTerminalError(t *testing.T, tt decideTerminalErrorCase) {
	t.Helper()

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
