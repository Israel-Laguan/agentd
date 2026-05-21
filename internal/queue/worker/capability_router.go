package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// Capability intent categories for external routing.
const (
	IntentGenerateImage    = "generate_image"
	IntentRealTimeSearch   = "real_time_search"
	IntentBrowseURL          = "browse_url"
	IntentSpreadsheetOps     = "spreadsheet_ops"
)

var (
	generateImageSignals   = []string{"image", "picture", "logo", "illustration", "render", "draw"}
	realTimeSearchSignals  = []string{"latest", "today", "current", "news", "real-time", "search the web"}
	browseURLSignals       = []string{"url", "http", "browse", "fetch page", "scrape"}
	spreadsheetOpsSignals  = []string{"spreadsheet", "excel", "csv", "sheet", "pivot"}

	capabilityIntentSignals  map[string][]string
	capabilityIntentMatchers map[string]map[string]*regexp.Regexp
)

func init() {
	capabilityIntentSignals = map[string][]string{
		IntentGenerateImage:   generateImageSignals,
		IntentRealTimeSearch:  realTimeSearchSignals,
		IntentBrowseURL:       browseURLSignals,
		IntentSpreadsheetOps:  spreadsheetOpsSignals,
	}
	capabilityIntentMatchers = make(map[string]map[string]*regexp.Regexp, len(capabilityIntentSignals))
	for intent, keywords := range capabilityIntentSignals {
		matchers := make(map[string]*regexp.Regexp, len(keywords))
		for _, kw := range keywords {
			matchers[kw] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(kw) + `\b`)
		}
		capabilityIntentMatchers[intent] = matchers
	}
}

// IntentClassification is the output of keyword-based capability intent classification.
type IntentClassification struct {
	Intent     string
	Confidence float64
	Scores     map[string]int
}

// IntentClassifier scores task text into capability intent categories.
type IntentClassifier struct {
	minConfidence float64
}

// NewIntentClassifier returns a classifier with the given minimum confidence threshold.
func NewIntentClassifier(minConfidence float64) *IntentClassifier {
	return &IntentClassifier{minConfidence: minConfidence}
}

// Classify scores task title and description into an intent with confidence.
func (c *IntentClassifier) Classify(task models.Task) IntentClassification {
	text := strings.ToLower(task.Title + " " + task.Description)
	scores := make(map[string]int, len(capabilityIntentSignals))
	for intent, keywords := range capabilityIntentSignals {
		for _, kw := range keywords {
			scores[intent] += countCapabilityIntentKeyword(text, intent, kw)
		}
	}

	topIntent, topScore, secondScore := topTwoCapabilityScores(scores)
	confidence := 0.0
	if topScore > 0 {
		confidence = float64(topScore-secondScore) / float64(topScore)
		if confidence > 1 {
			confidence = 1
		}
	}

	if topScore == 0 || confidence < c.minConfidence {
		return IntentClassification{Confidence: confidence, Scores: scores}
	}
	return IntentClassification{
		Intent:     topIntent,
		Confidence: confidence,
		Scores:     scores,
	}
}

func countCapabilityIntentKeyword(text, intent, kw string) int {
	matchers := capabilityIntentMatchers[intent]
	if matchers == nil {
		return 0
	}
	re := matchers[kw]
	if re == nil {
		return 0
	}
	return len(re.FindAllStringIndex(text, -1))
}

func topTwoCapabilityScores(scores map[string]int) (topIntent string, top, second int) {
	for intent, score := range scores {
		if score > top {
			second = top
			top = score
			topIntent = intent
		} else if score > second {
			second = score
		}
	}
	return topIntent, top, second
}

// CapabilityRouteDecision describes an external adapter dispatch.
type CapabilityRouteDecision struct {
	Intent  string
	Adapter string
	Tool    string
	Args    map[string]any
}

// CapabilityRouter maps classified intents to external capability adapters.
type CapabilityRouter struct {
	cfg        config.CapabilityRoutingConfig
	classifier *IntentClassifier
}

// NewCapabilityRouter returns a router when enabled with mappings; otherwise nil.
func NewCapabilityRouter(cfg config.CapabilityRoutingConfig) *CapabilityRouter {
	if !cfg.Enabled || len(cfg.Mappings) == 0 {
		return nil
	}
	mappings := make(map[string]string, len(cfg.Mappings))
	for k, v := range cfg.Mappings {
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if key != "" && val != "" {
			mappings[key] = val
		}
	}
	if len(mappings) == 0 {
		return nil
	}
	tools := make(map[string]string, len(cfg.Tools))
	for k, v := range cfg.Tools {
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if key != "" && val != "" {
			tools[key] = val
		}
	}
	return &CapabilityRouter{
		cfg: config.CapabilityRoutingConfig{
			Enabled:       cfg.Enabled,
			MinConfidence: cfg.MinConfidence,
			Mappings:      mappings,
			Tools:         tools,
		},
		classifier: NewIntentClassifier(cfg.MinConfidence),
	}
}

// Route resolves a capability route for the task when intent matches a configured mapping.
func (r *CapabilityRouter) Route(task models.Task, profile models.AgentProfile) (CapabilityRouteDecision, bool) {
	if r == nil {
		return CapabilityRouteDecision{}, false
	}

	var classification IntentClassification
	forced := strings.ToLower(strings.TrimSpace(profile.CapabilityRouteIntent))
	if forced != "" {
		classification = IntentClassification{Intent: forced, Confidence: 1}
	} else {
		classification = r.classifier.Classify(task)
	}
	if classification.Intent == "" {
		return CapabilityRouteDecision{}, false
	}

	adapter, ok := r.cfg.Mappings[classification.Intent]
	if !ok || adapter == "" {
		return CapabilityRouteDecision{}, false
	}

	tool := classification.Intent
	if override, ok := r.cfg.Tools[classification.Intent]; ok && override != "" {
		tool = override
	}

	decision := CapabilityRouteDecision{
		Intent:  classification.Intent,
		Adapter: adapter,
		Tool:    tool,
	}
	decision.Args = r.AdaptTask(task, decision)

	slog.Debug("capability routing: matched intent",
		"task_id", task.ID,
		"intent", classification.Intent,
		"confidence", classification.Confidence,
		"adapter", adapter,
		"tool", tool,
	)
	return decision, true
}

// AdaptTask builds default tool arguments from the task prompt.
func (r *CapabilityRouter) AdaptTask(task models.Task, decision CapabilityRouteDecision) map[string]any {
	_ = r
	_ = decision
	prompt := strings.TrimSpace(task.Title)
	if desc := strings.TrimSpace(task.Description); desc != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += desc
	}
	return map[string]any{"prompt": prompt}
}

func encodeCapabilityResult(out any) (string, error) {
	if s, ok := out.(string); ok {
		return s, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("capability result encode: %w", err)
	}
	return string(encoded), nil
}

// tryExternalCapabilityRoute classifies the task, dispatches to an external adapter when
// configured, commits session history, and returns without entering the agentic turn loop.
func (w *Worker) tryExternalCapabilityRoute(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
) (LoopResult, bool) {
	_ = project
	if w.capabilityRouter == nil {
		return LoopResult{}, false
	}

	decision, ok := w.capabilityRouter.Route(task, profile)
	if !ok {
		return LoopResult{}, false
	}

	if w.capabilities == nil {
		slog.Warn("capability routing: no capability registry; falling through to agentic loop",
			"task_id", task.ID,
			"intent", decision.Intent,
			"adapter", decision.Adapter,
		)
		return LoopResult{}, false
	}

	adapter, found := w.capabilities.GetAdapter(decision.Adapter)
	if !found || adapter == nil {
		slog.Warn("capability routing: adapter not registered; falling through to agentic loop",
			"task_id", task.ID,
			"intent", decision.Intent,
			"adapter", decision.Adapter,
		)
		return LoopResult{}, false
	}

	out, err := w.capabilities.CallTool(ctx, decision.Adapter, decision.Tool, decision.Args)
	if err != nil {
		w.failHard(ctx, task, fmt.Errorf("capability routing: %s/%s: %w", decision.Adapter, decision.Tool, err))
		return LoopResult{}, false
	}

	text, err := encodeCapabilityResult(out)
	if err != nil {
		w.failHard(ctx, task, err)
		return LoopResult{}, false
	}

	if w.messageEditor != nil {
		w.messageEditor.Commit(messages, gateway.PromptMessage{
			Role:    "assistant",
			Content: text,
		})
	} else if messages != nil {
		*messages = append(*messages, gateway.PromptMessage{
			Role:    "assistant",
			Content: text,
		})
	}

	w.commitTextWithProfile(ctx, task, text, &profile)
	return LoopResult{Status: LoopSuccessfulCompletion}, true
}
