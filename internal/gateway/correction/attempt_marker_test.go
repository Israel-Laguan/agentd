package correction

import (
	"context"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type seqGateway struct {
	replies []string
	seen    []spec.AttemptKind
}

func (g *seqGateway) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	g.seen = append(g.seen, req.Attempt)
	r := g.replies[len(g.seen)-1]
	return spec.AIResponse{Content: r}, nil
}

func (*seqGateway) Embed(context.Context, spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.EmbedResponse{}, nil
}

func (*seqGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) { return nil, nil }

func (*seqGateway) AnalyzeScope(context.Context, string) (*spec.ScopeAnalysis, error) {
	return nil, nil
}

func (*seqGateway) ClassifyIntent(context.Context, string) (*spec.IntentAnalysis, error) {
	return nil, nil
}

func TestCorrectionRetriesAreMarkedAsRepairs(t *testing.T) {
	gw := &seqGateway{replies: []string{"nope", `{"a":1}`}}
	req := spec.AIRequest{Purpose: spec.PurposeCreateElicit}
	if _, err := GenerateJSON[map[string]int](context.Background(), gw, req); err != nil {
		t.Fatal(err)
	}
	if len(gw.seen) != 2 {
		t.Fatalf("calls = %d, want 2", len(gw.seen))
	}
	if gw.seen[0] == spec.AttemptJSONRepair {
		t.Error("first try must not be marked as a repair")
	}
	if gw.seen[1] != spec.AttemptJSONRepair {
		t.Errorf("retry attempt = %q, want %q", gw.seen[1], spec.AttemptJSONRepair)
	}
}
