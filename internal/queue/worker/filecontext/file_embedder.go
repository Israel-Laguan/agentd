package wfilecontext

import (
	"context"
	"errors"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
)

// Embedder produces dense vectors for relevance scoring.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// GatewayEmbedder adapts gateway.AIGateway embedding calls.
type GatewayEmbedder struct {
	Gateway gateway.AIGateway
	Model   string
}

func (g *GatewayEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if g == nil {
		return nil, errors.New("embedder is nil")
	}
	if g.Gateway == nil {
		return nil, errors.New("gateway is nil")
	}
	resp, err := g.Gateway.Embed(ctx, spec.EmbedRequest{
		Input: texts,
		Model: g.Model,
	})
	if err != nil {
		return nil, err
	}
	return resp.Vectors, nil
}

var _ Embedder = (*GatewayEmbedder)(nil)
