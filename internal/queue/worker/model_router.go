package worker

import (
	"encoding/json"
	"regexp"
	"strings"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// charsPerToken is the approximate character-to-token ratio used for context-size override.
const charsPerToken = 4

var (
	reasoningSignals  = []string{"reason", "compare", "design", "architect", "analyse", "analyze", "analysis"}
	creativeSignals   = []string{"write", "draft", "generate"}
	mechanicalSignals = []string{"format", "summarize", "list", "rename", "grammar"}

	keywordMatchers map[string]*regexp.Regexp
)

func init() {
	all := append(append([]string{}, reasoningSignals...), creativeSignals...)
	all = append(all, mechanicalSignals...)
	keywordMatchers = make(map[string]*regexp.Regexp, len(all))
	for _, kw := range all {
		keywordMatchers[kw] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(kw) + `\b`)
	}
}

func countKeyword(text, kw string) int {
	re := keywordMatchers[kw]
	if re == nil {
		return 0
	}
	return len(re.FindAllStringIndex(text, -1))
}

// ComplexityScorer scores task text for model tier routing (0–10).
type ComplexityScorer struct{}

// ScoreTask returns a complexity score from task title and description.
func (ComplexityScorer) ScoreTask(task models.Task) int {
	text := strings.ToLower(task.Title + " " + task.Description)
	score := 0
	for _, kw := range reasoningSignals {
		score += 2 * countKeyword(text, kw)
	}
	for _, kw := range creativeSignals {
		score += countKeyword(text, kw)
	}
	for _, kw := range mechanicalSignals {
		score -= countKeyword(text, kw)
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

func firstConfiguredTier(candidates ...config.ModelTierTarget) (config.ModelTierTarget, bool) {
	for _, t := range candidates {
		if tierConfigured(t) {
			return t, true
		}
	}
	return config.ModelTierTarget{}, false
}

// totalToolChars approximates tool-definition size for context token estimation.
func totalToolChars(tools []gateway.ToolDefinition) int {
	total := 0
	for _, tool := range tools {
		if b, err := json.Marshal(tool); err == nil {
			total += len(b)
			continue
		}
		total += len(tool.Name) + len(tool.Description)
	}
	return total
}

// EstimateContextTokens approximates token count from messages and optional tool definitions.
func EstimateContextTokens(messages []gateway.PromptMessage, tools []gateway.ToolDefinition) int {
	chars := totalChars(messages) + totalToolChars(tools)
	return chars / charsPerToken
}

// Route selects provider and model for a task. ok is false when no tier is configured.
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
		tier, ok = firstConfiguredTier(r.cfg.High, r.cfg.Mid, r.cfg.Cheap)
		if !ok {
			return "", "", false
		}
	}
	return tier.Provider, tier.Model, true
}

// applyModelRouting selects provider/model from complexity routing when enabled.
func (w *Worker) applyModelRouting(
	task models.Task,
	profile models.AgentProfile,
	messages []gateway.PromptMessage,
	tools []gateway.ToolDefinition,
) models.AgentProfile {
	if w.modelRouter == nil {
		return profile
	}
	contextTokens := EstimateContextTokens(messages, tools)
	provider, model, ok := w.modelRouter.Route(task, contextTokens)
	if !ok {
		return profile
	}
	profile.Provider = provider
	profile.Model = model
	return profile
}
