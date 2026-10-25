package intake

import (
	"context"

	"agentd/internal/gateway/spec"
)

// fakeGateway answers Generate from replies in order (the last reply repeats)
// and records each request. Only Generate is used on these paths, so the
// embedded nil interface is never dereferenced.
type fakeGateway struct {
	spec.AIGateway
	replies []string
	seen    []spec.AIRequest
}

func (g *fakeGateway) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	g.seen = append(g.seen, req)
	i := min(len(g.seen)-1, len(g.replies)-1)
	return spec.AIResponse{Content: g.replies[i]}, nil
}
