package routing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agentd/internal/api/correlation"
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
		correlation.Logger(ctx).WarnContext(ctx, "provider does not support tools, falling back to legacy mode", "provider", string(p.Name()))
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

func appendCandidate(candidates []providers.Backend, p providers.Backend, selectedHasToolSupport *bool) []providers.Backend {
	if p.Capabilities().SupportsChatTools {
		*selectedHasToolSupport = true
	}
	return append(candidates, p)
}

// selectRoleRoutedCandidates prefers the role-assigned provider but allows cascade
// fallback through gateway.order on failure.
func (r *Router) selectRoleRoutedCandidates(requestedProvider string) (candidates []providers.Backend, matchedProvider bool, selectedHasToolSupport bool) {
	for _, p := range r.providers {
		if strings.EqualFold(requestedProvider, string(p.Name())) {
			matchedProvider = true
			candidates = appendCandidate(candidates, p, &selectedHasToolSupport)
		}
	}
	for _, p := range r.providers {
		if !strings.EqualFold(requestedProvider, string(p.Name())) {
			candidates = appendCandidate(candidates, p, &selectedHasToolSupport)
		}
	}
	return candidates, matchedProvider, selectedHasToolSupport
}

func skipExplicitProviderWithoutTools(requestedProvider string, req spec.AIRequest, hasRequestedTools bool, p providers.Backend) bool {
	// When an explicit provider is requested with tools but doesn't support them,
	// skip it so the after-loop error fires. Legacy fallback only applies to
	// non-explicit provider cascading (including role-routed providers).
	return requestedProvider != "" && !req.ProviderFromRole && hasRequestedTools && !p.Capabilities().SupportsChatTools
}

func (r *Router) selectDefaultCandidates(req spec.AIRequest, requestedProvider string, hasRequestedTools bool) (candidates []providers.Backend, matchedProvider bool, selectedHasToolSupport bool) {
	for _, p := range r.providers {
		if requestedProvider != "" && !strings.EqualFold(requestedProvider, string(p.Name())) {
			continue
		}
		matchedProvider = true
		if skipExplicitProviderWithoutTools(requestedProvider, req, hasRequestedTools, p) {
			continue
		}
		candidates = appendCandidate(candidates, p, &selectedHasToolSupport)
	}
	return candidates, matchedProvider, selectedHasToolSupport
}

func (r *Router) selectCandidateProviders(req spec.AIRequest) (candidates []providers.Backend, matchedProvider bool, selectedHasToolSupport bool) {
	requestedProvider := strings.TrimSpace(req.Provider)
	if req.ProviderFromRole && requestedProvider != "" {
		return r.selectRoleRoutedCandidates(requestedProvider)
	}
	return r.selectDefaultCandidates(req, requestedProvider, len(req.Tools) > 0)
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
	log := correlation.Logger(ctx)
	log.DebugContext(ctx, "router cascade starting",
		"provider", req.Provider, "model", req.Model, "role", string(req.Role),
		"json_mode", req.JSONMode, "tools_requested", hasRequestedTools,
		"candidates", len(candidates), "matched_provider", matchedProvider,
		"selected_has_tool_support", selectedHasToolSupport,
	)
	for _, p := range candidates {
		log.DebugContext(ctx, "router trying provider",
			"provider", string(p.Name()), "supports_tools", p.Capabilities().SupportsChatTools)
		resp, ok, err := r.tryProvider(ctx, p, req, hasRequestedTools, selectedHasToolSupport)
		if err != nil {
			log.DebugContext(ctx, "router provider error",
				"provider", string(p.Name()), "error", err.Error())
			var te errTruncation
			if errors.As(err, &te) {
				return spec.AIResponse{}, te.err
			}
			providerErrs = append(providerErrs, err)
			continue
		}
		if ok {
			log.DebugContext(ctx, "router provider success",
				"provider", string(p.Name()), "model", resp.ModelUsed, "tokens", resp.TokenUsage)
			r.recordBudget(req.TaskID, resp.TokenUsage)
			return resp, nil
		}
		log.DebugContext(ctx, "router provider skipped", "provider", string(p.Name()))
	}
	log.DebugContext(ctx, "router cascade exhausted",
		"provider", req.Provider, "errors", len(providerErrs))
	return spec.AIResponse{}, decideTerminalError(req, matchedProvider, selectedHasToolSupport, providerErrs)
}
