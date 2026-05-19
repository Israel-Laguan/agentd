package worker

import (
	"context"
	"fmt"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// maxIterationsGateway always returns tool calls, simulating a gateway that
// keeps requesting tool execution (used for testing max iterations)
type maxIterationsGateway struct {
	callCount int
	requests  []gateway.AIRequest
}

func (m *maxIterationsGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	m.requests = append(m.requests, req)
	m.callCount++
	return gateway.AIResponse{
		Content: fmt.Sprintf("Executing tool %d", m.callCount),
		ToolCalls: []gateway.ToolCall{
			{ID: fmt.Sprintf("call_%d", m.callCount), Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: fmt.Sprintf(`{"command": "echo %d"}`, m.callCount)}},
		},
	}, nil
}

func (m *maxIterationsGateway) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	return nil, nil
}

func (m *maxIterationsGateway) AnalyzeScope(ctx context.Context, userIntent string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (m *maxIterationsGateway) ClassifyIntent(ctx context.Context, userIntent string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

// tokenUsageGateway always returns tool calls with a fixed token usage per call.
type tokenUsageGateway struct {
	tokensPerCall int
	callCount     int
	requests      []gateway.AIRequest
}

func (g *tokenUsageGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	g.callCount++
	return gateway.AIResponse{
		Content: "Running tool",
		ToolCalls: []gateway.ToolCall{{
			ID:   fmt.Sprintf("call_%d", g.callCount),
			Type: "function",
			Function: gateway.ToolCallFunction{
				Name:      "bash",
				Arguments: `{"command": "echo budget"}`,
			},
		}},
		TokenUsage: g.tokensPerCall,
	}, nil
}

func (g *tokenUsageGateway) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *tokenUsageGateway) AnalyzeScope(ctx context.Context, userIntent string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *tokenUsageGateway) ClassifyIntent(ctx context.Context, userIntent string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

// sequenceGateway is a mock gateway that returns a predefined sequence of responses.
// Used for testing the agentic loop that requires multiple gateway calls.
type sequenceGateway struct {
	responses []gateway.AIResponse
	callCount int
	requests  []gateway.AIRequest
}

func (m *sequenceGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	m.requests = append(m.requests, req)

	if m.callCount >= len(m.responses) {
		// Return a final response without tool calls to break the loop
		return gateway.AIResponse{Content: "No more responses"}, nil
	}

	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *sequenceGateway) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	return nil, nil
}

func (m *sequenceGateway) AnalyzeScope(ctx context.Context, userIntent string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (m *sequenceGateway) ClassifyIntent(ctx context.Context, userIntent string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}
