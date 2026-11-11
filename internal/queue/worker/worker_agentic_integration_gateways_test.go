package worker

import (
	"context"

	"agentd/internal/gateway"
)

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

func (m *sequenceGateway) Embed(ctx context.Context, req gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}
