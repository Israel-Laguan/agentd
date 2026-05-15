package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
	"agentd/internal/models"
)

const defaultMaxMessageChars = 12000
const defaultMaxTasksPerPhase = 7

// Router cascades across providers and applies truncation and budgets.
type Router struct {
	providers        []providers.Backend
	maxMessageChars  int
	truncator        spec.Truncator
	maxTasksPerPhase int
	budget           spec.BudgetTracker
	roleRoutes       map[spec.Role]spec.RoleTarget
}

var _ spec.AIGateway = (*Router)(nil)

// NewRouter builds a router from explicit provider backends (tests and advanced wiring).
func NewRouter(providersList ...providers.Backend) *Router {
	return &Router{
		providers:        providersList,
		maxMessageChars:  defaultMaxMessageChars,
		truncator:        truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}},
		maxTasksPerPhase: defaultMaxTasksPerPhase,
	}
}

// NewRouterFromConfigs builds providers from ProviderConfig entries in order.
func NewRouterFromConfigs(configs []spec.ProviderConfig) *Router {
	list := make([]providers.Backend, 0, len(configs))
	for _, cfg := range configs {
		list = providers.AppendFromConfig(list, cfg)
	}
	return NewRouter(list...)
}

// WithTruncation sets the truncator and optional max message size override.
func (r *Router) WithTruncation(truncator spec.Truncator, maxMessageChars int) *Router {
	if truncator != nil {
		r.truncator = truncator
	}
	if maxMessageChars > 0 {
		r.maxMessageChars = maxMessageChars
	}
	return r
}

// WithPhaseCap configures how many tasks may appear in a single generated plan segment.
func (r *Router) WithPhaseCap(maxTasksPerPhase int) *Router {
	if maxTasksPerPhase >= 0 {
		r.maxTasksPerPhase = maxTasksPerPhase
	}
	return r
}

// WithBudget sets optional per-task token accounting.
func (r *Router) WithBudget(tracker spec.BudgetTracker) *Router {
	r.budget = tracker
	return r
}

// WithRoleRouting maps logical roles to preferred provider/model pairs.
func (r *Router) WithRoleRouting(routes map[spec.Role]spec.RoleTarget) *Router {
	r.roleRoutes = routes
	return r
}

// Generate implements spec.AIGateway.
func (r *Router) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if req.JSONMode {
		return r.generateJSON(ctx, req)
	}
	req.Messages = mergeHouseRulesIntoMessages(req.Messages, HouseRulesFromContext(ctx))
	return r.generateOnce(ctx, req)
}

func (r *Router) reserveBudget(taskID string) error {
	if r.budget != nil && taskID != "" {
		return r.budget.Reserve(taskID)
	}
	return nil
}

func (r *Router) recordBudget(taskID string, usage int) {
	if r.budget != nil && taskID != "" {
		r.budget.Add(taskID, usage)
	}
}

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
	hasRequestedTools := len(req.Tools) > 0
	for _, p := range r.providers {
		if req.Provider != "" && req.Provider != string(p.Name()) {
			continue
		}
		matchedProvider = true
		// When an explicit provider is requested with tools but doesn't support them,
		// skip it so the after-loop error fires. Legacy fallback only applies to
		// non-explicit provider cascading.
		if req.Provider != "" && hasRequestedTools && !p.Capabilities().SupportsChatTools {
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
	hasRequestedTools := len(req.Tools) > 0
	if req.Provider != "" && matchedProvider && hasRequestedTools && !selectedHasToolSupport {
		return fmt.Errorf("provider %q does not support tools, use a different provider or disable agentic mode", req.Provider)
	}
	if req.Provider != "" && len(providerErrs) == 0 {
		return fmt.Errorf("LLM provider %q is not configured", req.Provider)
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

func (r *Router) applyRoleRouting(req spec.AIRequest) spec.AIRequest {
	if r.roleRoutes == nil || req.Role == "" {
		return req
	}
	target, ok := r.roleRoutes[req.Role]
	if !ok {
		return req
	}
	if req.Provider == "" && target.Provider != "" {
		req.Provider = target.Provider
	}
	if req.Model == "" && target.Model != "" {
		req.Model = target.Model
	}
	return req
}

func (r *Router) applyTruncation(ctx context.Context, messages []spec.PromptMessage, providerBudget int) ([]spec.PromptMessage, error) {
	budget := providerBudget
	if budget <= 0 {
		budget = r.maxMessageChars
	}
	trunc := r.truncator
	if trunc == nil {
		trunc = truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}}
	}
	return trunc.Apply(ctx, messages, budget)
}

func (r *Router) generateJSON(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	req.JSONMode = true
	req.Messages = mergeHouseRulesIntoMessages(req.Messages, HouseRulesFromContext(ctx))
	var lastResp spec.AIResponse
	var lastErr error
	for attempt := 0; attempt < correction.MaxJSONAttempts; attempt++ {
		resp, err := r.generateOnce(ctx, req)
		if err != nil {
			return spec.AIResponse{}, err
		}
		if json.Valid([]byte(resp.Content)) {
			return resp, nil
		}
		lastResp = resp
		lastErr = fmt.Errorf("invalid character in JSON response")
		if err := json.Unmarshal([]byte(resp.Content), &map[string]any{}); err != nil {
			lastErr = err
		}
		req.Messages = append(req.Messages, correction.PromptAfterInvalidJSON(lastErr))
	}
	return spec.AIResponse{}, correction.WrapInvalidJSONError(lastErr, lastResp.Content)
}
