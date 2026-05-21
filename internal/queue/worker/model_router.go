package worker

import (
	"strings"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// charsPerToken is the approximate character-to-token ratio used for context-size override.
const charsPerToken = 4

var (
	reasoningSignals  = []string{"reason", "compare", "design", "architect", "analyse"}
	creativeSignals   = []string{"write", "draft", "generate"}
	mechanicalSignals = []string{"format", "summarize", "list", "rename", "fix grammar"}
)

// ComplexityScorer scores task text for model tier routing (0–10).
type ComplexityScorer struct{}

// ScoreTask returns a complexity score from task title and description.
func (ComplexityScorer) ScoreTask(task models.Task) int {
	text := strings.ToLower(task.Title + " " + task.Description)
	score := 0
	for _, kw := range reasoningSignals {
		score += 2 * strings.Count(text, kw)
	}
	for _, kw := range creativeSignals {
		score += strings.Count(text, kw)
	}
	for _, kw := range mechanicalSignals {
		score -= strings.Count(text, kw)
	}
	if score < 0 {
		return 0
	}
	if score > 10 {
		return 10
	}
	return score
}

// ModelRouter maps complexity scores and context size to configured model tiers.
type ModelRouter struct {
	cfg config.ModelRoutingConfig
}

// NewModelRouter returns a router when enabled and at least one tier has provider+model.
func NewModelRouter(cfg config.ModelRoutingConfig) *ModelRouter {
	if !cfg.Enabled {
		return nil
	}
	if !tierConfigured(cfg.Cheap) && !tierConfigured(cfg.Mid) && !tierConfigured(cfg.High) {
		return nil
	}
	return &ModelRouter{cfg: cfg}
}

func tierConfigured(t config.ModelTierTarget) bool {
	return t.Provider != "" && t.Model != ""
}

// IsPinned reports whether the profile has an explicit model that disables routing.
func IsPinned(profile models.AgentProfile) bool {
	return profile.Model != ""
}

// EstimateContextTokens approximates token count from prompt messages (chars / 4).
func EstimateContextTokens(messages []gateway.PromptMessage) int {
	chars := totalChars(messages)
	return chars / charsPerToken
}

// Route selects provider and model for a task. ok is false when the chosen tier is unset.
func (r *ModelRouter) Route(task models.Task, contextTokens int) (provider, model string, ok bool) {
	if r == nil {
		return "", "", false
	}
	tier := r.cfg.High
	if contextTokens < r.cfg.ContextTokenThreshold {
		score := (ComplexityScorer{}).ScoreTask(task)
		switch {
		case score <= 2:
			tier = r.cfg.Cheap
		case score <= 6:
			tier = r.cfg.Mid
		default:
			tier = r.cfg.High
		}
	}
	if !tierConfigured(tier) {
		return "", "", false
	}
	return tier.Provider, tier.Model, true
}

// applyModelRouting selects provider/model from complexity routing when enabled and unpinned.
func (w *Worker) applyModelRouting(task models.Task, profile models.AgentProfile, messages []gateway.PromptMessage) models.AgentProfile {
	if w.modelRouter == nil || IsPinned(profile) {
		return profile
	}
	contextTokens := EstimateContextTokens(messages)
	provider, model, ok := w.modelRouter.Route(task, contextTokens)
	if !ok {
		return profile
	}
	profile.Provider = provider
	profile.Model = model
	return profile
}
