package worker

import (
	"log/slog"
	"strings"

	"agentd/internal/config"
	"agentd/internal/models"
)

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
func (r *CapabilityRouter) AdaptTask(task models.Task, _ CapabilityRouteDecision) map[string]any {
	prompt := strings.TrimSpace(task.Title)
	if desc := strings.TrimSpace(task.Description); desc != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += desc
	}
	return map[string]any{"prompt": prompt}
}
