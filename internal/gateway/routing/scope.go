package routing

import (
	"context"
	"fmt"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

const scopeAnalyzerSystemPrompt = "You are the Frontdesk scope analyzer. Return ONLY strict JSON with keys single_scope, confidence, scopes, reason. A scope means one product or one cohesive deliverable. If the request contains multiple distinct deliverables, set single_scope=false and return one scope item per deliverable with stable id and user-friendly label. If it is one scope, set single_scope=true and still return scopes with exactly one entry mirroring the user intent. confidence is 0..1 and reason is one short sentence."

// AnalyzeScope implements spec.AIGateway.
func (r *Router) AnalyzeScope(ctx context.Context, userIntent string) (*spec.ScopeAnalysis, error) {
	req := spec.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: scopeAnalyzerSystemPrompt},
			{Role: "user", Content: userIntent},
		},
		Temperature: 0.1,
		JSONMode:    true,
		Role:        spec.RoleChat,
	}
	analysis, err := correction.GenerateJSON[spec.ScopeAnalysis](ctx, r, req)
	if err != nil {
		return nil, err
	}
	if !analysis.SingleScope && len(analysis.Scopes) == 0 {
		return nil, fmt.Errorf("%w: missing scopes for multi-scope classification", models.ErrInvalidJSONResponse)
	}
	return &analysis, nil
}
