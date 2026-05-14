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

// detectToolSupport scans the provider list for tool support among selected providers.
func (r *Router) detectToolSupport(req spec.AIRequest) bool {
	for _, p := range r.providers {
		if req.Provider != "" && req.Provider != string(p.Name()) {
			continue
		}
		if p.Capabilities().SupportsChatTools {
			return true
		}
	}
	return false
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
			return spec.AIResponse{}, false, err
		}
		req.Messages = messages
	}
	resp, err := p.Generate(ctx, req)
	if err != nil {
		return spec.AIResponse{}, false, err
	}
	return resp, true, nil
}

func (r *Router) generateOnce(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if len(r.providers) == 0 {
		return spec.AIResponse{}, errors.New("no LLM providers configured")
	}
	if err := r.reserveBudget(req.TaskID); err != nil {
		return spec.AIResponse{}, err
	}
	req = r.applyRoleRouting(req)
	hasRequestedTools := len(req.Tools) > 0
	selectedHasToolSupport := hasRequestedTools && r.detectToolSupport(req)
	var providerErrs []error
	for _, p := range r.providers {
		if req.Provider != "" && req.Provider != string(p.Name()) {
			continue
		}
		if req.Provider != "" && len(req.Tools) > 0 && !p.Capabilities().SupportsChatTools {
			return spec.AIResponse{}, fmt.Errorf("provider %q does not support tools, use a different provider or disable agentic mode", req.Provider)
		}
		resp, ok, err := r.tryProvider(ctx, p, req, hasRequestedTools, selectedHasToolSupport)
		if err != nil {
			providerErrs = append(providerErrs, err)
			continue
		}
		if ok {
			r.recordBudget(req.TaskID, resp.TokenUsage)
			return resp, nil
		}
	}
	if req.Provider != "" && len(providerErrs) == 0 {
		return spec.AIResponse{}, fmt.Errorf("LLM provider %q is not configured", req.Provider)
	}
	return spec.AIResponse{}, fmt.Errorf("%w: %v", models.ErrLLMUnreachable, errors.Join(providerErrs...))
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
