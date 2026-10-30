package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

const defaultMaxMessageChars = 12000

// Router cascades across providers and applies truncation and budgets.
type Router struct {
	providers       []providers.Backend
	providerModels  map[string][]string
	maxMessageChars int
	truncator       spec.Truncator
	budget          spec.BudgetTracker
	routes          route.Table
	recorder        spec.Recorder
}

var _ spec.AIGateway = (*Router)(nil)

// NewRouter builds a router from explicit provider backends (tests and advanced wiring).
func NewRouter(providersList ...providers.Backend) *Router {
	return &Router{
		providers:       providersList,
		maxMessageChars: defaultMaxMessageChars,
		truncator:       truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}},
	}
}

// ProviderNames returns the names of all configured backends in router order.
// Returns nil for a nil receiver.
func (r *Router) ProviderNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.providers))
	for _, p := range r.providers {
		if p != nil {
			names = append(names, string(p.Name()))
		}
	}
	return names
}

// KnownModels returns gateway-configured model hints for a provider name (config default
// and routed agents' models). Empty when the provider is unknown or no models are configured.
func (r *Router) KnownModels(provider string) []string {
	if r == nil {
		return nil
	}
	return knownModelsForProvider(r.providerModels, provider)
}

// ProviderSupportsChatTools reports whether the named configured backend supports
// chat tool round-tripping. Custom provider names (e.g. poolside with adapter openai)
// are resolved via backend Capabilities(). When provider is empty, returns true if
// any configured backend in gateway order supports chat tools (cascade / seed profiles).
// Returns false for a nil receiver.
func (r *Router) ProviderSupportsChatTools(provider string) bool {
	if r == nil {
		return false
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		for _, p := range r.providers {
			if p.Capabilities().SupportsChatTools {
				return true
			}
		}
		return false
	}
	for _, p := range r.providers {
		if strings.EqualFold(string(p.Name()), provider) {
			return p.Capabilities().SupportsChatTools
		}
	}
	slog.Debug("ProviderSupportsChatTools: provider not found in router, returning false",
		"provider", provider)
	return false
}

// NewRouterFromConfigs builds providers from ProviderConfig entries in order.
func NewRouterFromConfigs(configs []spec.ProviderConfig) (*Router, error) {
	list := make([]providers.Backend, 0, len(configs))
	for _, cfg := range configs {
		var err error
		list, err = providers.AppendFromConfig(list, cfg)
		if err != nil {
			return nil, err
		}
	}
	router := NewRouter(list...)
	router.providerModels = buildProviderModels(configs)
	return router, nil
}

// RunToolProbes runs runtime tool-support probes on backends that implement
// ToolProber. Each probe is self-gating (opt-in via config) and non-fatal;
// errors are logged and treated as "not supported".
func (r *Router) RunToolProbes(ctx context.Context) {
	if r == nil {
		return
	}
	for _, p := range r.providers {
		if p == nil {
			continue
		}
		if prober, ok := p.(providers.ToolProber); ok {
			prober.ProbeTools(ctx)
		}
	}
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

// WithBudget sets optional per-task token accounting.
func (r *Router) WithBudget(tracker spec.BudgetTracker) *Router {
	r.budget = tracker
	return r
}

// WithPurposeRouting sets the validated agents and stages table that picks the
// agent for each call. An empty table leaves every call on the cascade.
func (r *Router) WithPurposeRouting(table route.Table) *Router {
	r.routes = table
	if r.providerModels == nil {
		r.providerModels = make(map[string][]string)
	}
	mergeAgentModels(r.providerModels, table.Agents)
	return r
}

// Generate implements spec.AIGateway.
func (r *Router) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	req = r.routeRequest(req)
	if req.JSONMode {
		return r.generateJSON(ctx, req)
	}
	req.Messages = mergeHouseRulesIntoMessages(req.Messages, HouseRulesFromContext(ctx))
	return r.generateOnce(ctx, req)
}

// Embed implements spec.AIGateway by delegating to the first EmbedBackend provider.
func (r *Router) Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	var errs []error
	attempts := 0
	for _, p := range r.providers {
		eb, ok := p.(providers.EmbedBackend)
		if !ok {
			continue
		}
		started := time.Now()
		resp, err := eb.Embed(ctx, req)
		r.emit(ctx, embedRecord(p, req, started, err, attempts > 0))
		attempts++
		if err == nil {
			return resp, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return spec.EmbedResponse{}, fmt.Errorf("no embedding provider configured")
	}
	return spec.EmbedResponse{}, errors.Join(errs...)
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

// routeRequest makes the one routing decision for a call and logs it. A pin or
// the cascade changes nothing. A rule or stage default fills in the provider the
// caller left empty; the provider is tried first and the cascade still follows
// on failure. The model comes with the provider it belongs to.
func (r *Router) routeRequest(req spec.AIRequest) spec.AIRequest {
	d := r.routes.Resolve(req.Purpose, req.AgentID, req.Attrs)
	level := slog.LevelInfo
	if d.Rule == route.RuleCascade {
		level = slog.LevelDebug
	}
	slog.Log(context.Background(), level, "routing decision", "purpose", d.Purpose, "required_level", d.Required,
		"rule", d.Rule, "agent", d.Agent, "task_id", req.TaskID)
	req.RoutedAgent = d.Agent
	if d.Rule == route.RulePin || d.Agent == "" {
		return req
	}
	if strings.TrimSpace(req.Provider) == "" {
		req.Provider = d.Provider
		req.ProviderRouted = true
	}
	if strings.TrimSpace(req.Model) == "" && strings.EqualFold(strings.TrimSpace(req.Provider), d.Provider) {
		req.Model = d.Model
		req.ModelRouted = true
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
		if attempt > 0 {
			req.Attempt = spec.AttemptJSONRepair
		}
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
