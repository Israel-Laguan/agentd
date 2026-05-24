package routing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// errTruncation is a wrapper to distinguish truncation errors from provider errors
// so that generateOnce can return them immediately instead of cascading.
type errTruncation struct{ err error }

func (e errTruncation) Error() string { return e.err.Error() }
func (e errTruncation) Unwrap() error { return e.err }

// tryProvider attempts a single provider and returns (response, ok, error).
//   - ok=true, err=nil: success; the caller should use the response.
//   - ok=false, err=nil: the provider was skipped without error (e.g. a non-tool
//     provider when a tool-capable candidate exists). The caller should continue
//     to the next candidate and NOT accumulate an error.
//   - ok=false, err!=nil: provider failure; the caller may aggregate the error
//     and continue cascading.
func (r *Router) tryProvider(ctx context.Context, p providers.Backend, baseReq spec.AIRequest, hasRequestedTools bool, selectedHasToolSupport bool) (spec.AIResponse, bool, error) {
	req := baseReq
	if hasRequestedTools && !p.Capabilities().SupportsChatTools {
		if selectedHasToolSupport {
			return spec.AIResponse{}, false, nil
		}
		req.Tools = nil
		req.JSONMode = true
		slog.Warn("provider does not support tools, falling back to legacy mode", "provider", string(p.Name()))
	}
	if !req.SkipTruncation {
		messages, err := r.applyTruncation(ctx, req.Messages, p.MaxInputChars())
		if err != nil {
			return spec.AIResponse{}, false, errTruncation{err}
		}
		req.Messages = messages
	}
	resp, err := p.Generate(ctx, req)
	if err != nil {
		return spec.AIResponse{}, false, err
	}
	return resp, true, nil
}

func (r *Router) selectCandidateProviders(req spec.AIRequest) (candidates []providers.Backend, matchedProvider bool, selectedHasToolSupport bool) {
	requestedProvider := strings.TrimSpace(req.Provider)
	hasRequestedTools := len(req.Tools) > 0
	for _, p := range r.providers {
		if requestedProvider != "" && !strings.EqualFold(requestedProvider, string(p.Name())) {
			continue
		}
		matchedProvider = true
		// When an explicit provider is requested with tools but doesn't support them,
		// skip it so the after-loop error fires. Legacy fallback only applies to
		// non-explicit provider cascading (including role-routed providers).
		if requestedProvider != "" && !req.ProviderFromRole && hasRequestedTools && !p.Capabilities().SupportsChatTools {
			continue
		}
		candidates = append(candidates, p)
		if p.Capabilities().SupportsChatTools {
			selectedHasToolSupport = true
		}
	}
	return candidates, matchedProvider, selectedHasToolSupport
}

func decideTerminalError(req spec.AIRequest, matchedProvider, selectedHasToolSupport bool, providerErrs []error) error {
	requestedProvider := strings.TrimSpace(req.Provider)
	hasRequestedTools := len(req.Tools) > 0
	if requestedProvider != "" && !req.ProviderFromRole && matchedProvider && hasRequestedTools && !selectedHasToolSupport {
		return fmt.Errorf("provider %q does not support tools, use a different provider or disable agentic mode", requestedProvider)
	}
	if requestedProvider != "" && len(providerErrs) == 0 {
		return fmt.Errorf("LLM provider %q is not configured", requestedProvider)
	}
	if len(providerErrs) == 0 {
		return fmt.Errorf("%w: all providers skipped (tool mismatch or empty cascade)", models.ErrLLMUnreachable)
	}
	return fmt.Errorf("%w: %v", models.ErrLLMUnreachable, errors.Join(providerErrs...))
}

func (r *Router) generateOnce(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if len(r.providers) == 0 {
		return spec.AIResponse{}, errors.New("no LLM providers configured")
	}
	if err := r.reserveBudget(req.TaskID); err != nil {
		return spec.AIResponse{}, err
	}
	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req = r.applyRoleRouting(req)
	candidates, matchedProvider, selectedHasToolSupport := r.selectCandidateProviders(req)
	hasRequestedTools := len(req.Tools) > 0
	var providerErrs []error
	for _, p := range candidates {
		resp, ok, err := r.tryProvider(ctx, p, req, hasRequestedTools, selectedHasToolSupport)
		if err != nil {
			// Truncation errors are not provider-specific; return immediately.
			var te errTruncation
			if errors.As(err, &te) {
				return spec.AIResponse{}, te.err
			}
			providerErrs = append(providerErrs, err)
			continue
		}
		if ok {
			r.recordBudget(req.TaskID, resp.TokenUsage)
			return resp, nil
		}
	}
	return spec.AIResponse{}, decideTerminalError(req, matchedProvider, selectedHasToolSupport, providerErrs)
}
