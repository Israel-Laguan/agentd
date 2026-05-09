package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// ScopeClarification is returned when multiple scopes are detected.
type ScopeClarification struct {
	Kind    string                `json:"kind"`
	Message string                `json:"message"`
	Scopes  []gateway.ScopeOption `json:"scopes"`
}

// IntentClarification is returned when the user intent is ambiguous.
type IntentClarification struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// FeasibilityClarification is returned when the request is impossible, unsafe,
// or too vague to plan as a software project (vagueness shield).
type FeasibilityClarification struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}

// ErrMultipleApprovedScopes is returned when the caller passes more than one
// approved scope in a single turn.
var ErrMultipleApprovedScopes = errors.New("invalid approved scopes request")

// Planner routes user intent through classification, scope analysis, and plan
// generation. It is the primary entry point of Box 1 (Frontdesk).
type Planner struct {
	Gateway gateway.AIGateway
	// SettingsStore is optional. When set, row "house_rules" is injected into
	// planning LLM calls via gateway context (see models.SettingKeyHouseRules).
	SettingsStore models.KanbanStore
	Summarizer    *StatusSummarizer
	Stash         *FileStash
	Truncator     gateway.Truncator
	Budget        int
}

// PlanContent classifies the intent and produces a JSON response (plan, scope
// clarification, intent clarification, or status report).
func (p *Planner) PlanContent(
	ctx context.Context,
	approvedScopes []string,
	intent string,
	files []FileRef,
) ([]byte, error) {
	planCtx := p.ctxWithHouseRules(ctx)
	if len(approvedScopes) == 1 {
		return p.planApprovedScope(planCtx, intent, approvedScopes[0], files)
	}
	if len(approvedScopes) > 1 {
		return nil, ErrMultipleApprovedScopes
	}

	classification, err := p.Gateway.ClassifyIntent(ctx, intent)
	if err != nil {
		return nil, err
	}

	switch classification.Intent {
	case "status_check":
		return p.statusReport(ctx)
	case "out_of_scope":
		msg := "This request cannot be turned into a concrete software project for agentd, or it is too vague to plan safely. Describe a specific app, script, API, or refactor you want built in your workspace."
		if strings.TrimSpace(classification.Reason) != "" {
			msg += " (" + strings.TrimSpace(classification.Reason) + ")"
		}
		return marshalContent(FeasibilityClarification{
			Kind:    "feasibility_clarification",
			Message: msg,
			Reason:  classification.Reason,
		})
	case "plan_request":
		return p.analyzeAndPlan(planCtx, intent, files)
	default:
		return marshalContent(IntentClarification{
			Kind:    "intent_clarification",
			Message: "I'm not sure what you need. You can ask for a status update or describe work you'd like to plan.",
		})
	}
}

func (p *Planner) statusReport(ctx context.Context) ([]byte, error) {
	report, err := p.Summarizer.Summarize(ctx)
	if err != nil {
		return nil, err
	}
	return marshalContent(report)
}

func (p *Planner) analyzeAndPlan(ctx context.Context, intent string, files []FileRef) ([]byte, error) {
	analysis, err := p.Gateway.AnalyzeScope(ctx, intent)
	if err != nil {
		return nil, err
	}
	if !analysis.SingleScope {
		return marshalContent(ScopeClarification{
			Kind:    "scope_clarification",
			Message: "Multiple projects detected. Resend one turn per scope using approved_scopes.",
			Scopes:  analysis.Scopes,
		})
	}
	return p.planApprovedScope(ctx, intent, "", files)
}

func (p *Planner) planApprovedScope(ctx context.Context, intent, approvedScope string, files []FileRef) ([]byte, error) {
	planningIntent := strings.TrimSpace(intent)
	if approvedScope != "" {
		planningIntent += "\nRestrict planning to scope: " + approvedScope
	}
	if len(files) > 0 {
		withContents, err := IntentWithFileContents(ctx, p.Stash, p.Truncator, p.Budget, planningIntent, files)
		if err != nil {
			return nil, err
		}
		planningIntent = withContents
	}
	var (
		plan *models.DraftPlan
		err  error
	)
	if adapter, ok := any(p.Gateway).(gateway.ContractAdapter); ok {
		var draft models.DraftPlan
		if err = adapter.GenerateStructuredJSON(ctx, planningIntent, &draft); err == nil {
			plan = &draft
		}
	}
	if plan == nil && err == nil {
		plan, err = p.Gateway.GeneratePlan(ctx, planningIntent)
	}
	if err != nil {
		return nil, err
	}
	return marshalContent(plan)
}

func marshalContent(value any) ([]byte, error) {
	return json.Marshal(value)
}

func (p *Planner) ctxWithHouseRules(ctx context.Context) context.Context {
	if p.SettingsStore == nil {
		return ctx
	}
	rules := models.LoadHouseRules(ctx, p.SettingsStore)
	if rules == "" {
		return ctx
	}
	return gateway.WithHouseRules(ctx, rules)
}
