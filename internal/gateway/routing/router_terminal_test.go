package routing

import (
	"errors"
	"fmt"
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
	// B-016: the aggregate must keep the provider errors' identity in its chain,
	// not just their text. A provider answering 429 has to stay recognisable as
	// ErrLLMQuotaExceeded, because worker_handoffs.go's HandleGatewayError gates
	// the per-provider breaker registry on exactly that check.
	wantLLMQuotaExceeded bool
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
	{
		// B-016: every provider answered 429. The cascade is exhausted, so the
		// aggregate is still unreachable, and it is also a quota failure, so the
		// per-provider breaker registry must see it.
		name:                 "all providers quota exceeded",
		providerErrs:         []error{models.ErrLLMQuotaExceeded, fmt.Errorf("openai: %w", models.ErrLLMQuotaExceeded)},
		wantSubstr:           models.ErrLLMQuotaExceeded.Error(),
		wantLLMUnreachable:   true,
		wantLLMQuotaExceeded: true,
	},
	{
		// The mirror: an unreachable cascade must NOT read as a quota failure,
		// or a network outage would open per-provider breakers it has no evidence for.
		name:               "all providers unreachable",
		providerErrs:       []error{fmt.Errorf("dial: %w", models.ErrLLMUnreachable)},
		wantSubstr:         models.ErrLLMUnreachable.Error(),
		wantLLMUnreachable: true,
	},
	{
		// Mixed: both sentinels reach the chain, and HandleGatewayError checks
		// quota first, so this follows the provider-quota branch.
		name:                 "mixed quota and unreachable",
		providerErrs:         []error{models.ErrLLMQuotaExceeded, models.ErrLLMUnreachable},
		wantSubstr:           models.ErrLLMQuotaExceeded.Error(),
		wantLLMUnreachable:   true,
		wantLLMQuotaExceeded: true,
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
	// Both directions matter: a false positive opens a provider breaker for an
	// outage that never touched that provider's quota.
	if got := errors.Is(err, models.ErrLLMQuotaExceeded); got != tt.wantLLMQuotaExceeded {
		t.Fatalf("errors.Is(err, ErrLLMQuotaExceeded) = %v, want %v (err: %v)",
			got, tt.wantLLMQuotaExceeded, err)
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
