package worker

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

type attemptGateway struct {
	replies []string
	seen    []gateway.AttemptKind
}

func (g *attemptGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.seen = append(g.seen, req.Attempt)
	return gateway.AIResponse{Content: g.replies[len(g.seen)-1]}, nil
}

func (*attemptGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func (*attemptGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (*attemptGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (*attemptGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

func TestWorkPlanRetryIsMarkedAsRepair(t *testing.T) {
	gw := &attemptGateway{replies: []string{"not json", `{"steps":[{"id":"a","action":"do it"}]}`}}
	w := &Worker{gateway: gw}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, Title: "x"}
	if _, err := w.GeneratePlan(context.Background(), task, models.Project{}, nil); err != nil {
		t.Fatal(err)
	}
	if len(gw.seen) != 2 || gw.seen[0] == gateway.AttemptJSONRepair || gw.seen[1] != gateway.AttemptJSONRepair {
		t.Fatalf("attempt kinds = %v, want [first json_repair]", gw.seen)
	}
}
