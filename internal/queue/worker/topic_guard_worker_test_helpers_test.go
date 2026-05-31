package worker

import (
	"context"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type topicDriftGateway struct {
	response string
	calls    int
}

func (g *topicDriftGateway) Generate(_ context.Context, _ gateway.AIRequest) (gateway.AIResponse, error) {
	g.calls++
	return gateway.AIResponse{Content: g.response}, nil
}

func (g *topicDriftGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *topicDriftGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *topicDriftGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

func (g *topicDriftGateway) Embed(context.Context, spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.EmbedResponse{}, nil
}
