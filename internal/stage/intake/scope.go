package intake

import (
	"context"
	"fmt"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
)

// scopeAnalyzerSystemPrompt is the intake scope analyzer prompt. The e2e mock
// matches on "Frontdesk scope analyzer", so keep that wording.
const scopeAnalyzerSystemPrompt = "You are the Frontdesk scope analyzer. Return ONLY strict JSON with keys single_scope, confidence, scopes, reason. A scope means one product or one cohesive deliverable. If the request contains multiple distinct deliverables, set single_scope=false and return one scope item per deliverable with stable id and user-friendly label. If it is one scope, set single_scope=true and still return scopes with exactly one entry mirroring the user intent. confidence is 0..1 and reason is one short sentence."

// AnalyzeScope asks agent whether userIntent is one scope or several. A
// multi-scope answer with no scopes is rejected after the call, not by the
// contract.
func AnalyzeScope(ctx context.Context, agent stage.Agent, userIntent string) (*spec.ScopeAnalysis, error) {
	res, err := agent.Call(ctx, stage.StageRequest{
		Stage:   stage.StageIntake,
		Purpose: spec.PurposeIntakeAnalyzeScope,
		Context: []spec.PromptMessage{
			{Role: "system", Content: scopeAnalyzerSystemPrompt},
			{Role: "user", Content: userIntent},
		},
		Contract:    stage.NewContract[spec.ScopeAnalysis](),
		Temperature: 0.1,
	})
	if err != nil {
		return nil, err
	}
	analysis, err := decode[spec.ScopeAnalysis](res)
	if err != nil {
		return nil, err
	}
	if !analysis.SingleScope {
		if len(analysis.Scopes) == 0 {
			return nil, fmt.Errorf("%w: missing scopes for multi-scope classification", models.ErrInvalidJSONResponse)
		}
		for _, scope := range analysis.Scopes {
			if scope.ID == "" || scope.Label == "" {
				return nil, fmt.Errorf("%w: multi-scope entries require an id and label", models.ErrInvalidJSONResponse)
			}
		}
	}
	return analysis, nil
}
