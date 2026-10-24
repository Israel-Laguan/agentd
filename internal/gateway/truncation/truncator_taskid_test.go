package truncation

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

// taskIDSummaryGateway is a summaryGateway that also records the TaskID of
// each call.
type taskIDSummaryGateway struct {
	summaryGateway
	taskID string
}

func (g *taskIDSummaryGateway) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	g.taskID = req.TaskID
	return g.summaryGateway.Generate(ctx, req)
}

// T-057: the truncator's summary call belongs to the task of the call whose
// messages it shortens, which the router puts on ctx.
func TestSummarizeTruncatorSummaryCarriesTheContextTaskID(t *testing.T) {
	t.Parallel()

	gw := &taskIDSummaryGateway{summaryGateway: summaryGateway{content: "short summary"}}
	tr := SummarizeTruncator{Gateway: gw}
	ctx := spec.WithTaskID(context.Background(), "task-7")
	msgs := []spec.PromptMessage{{Role: "user", Content: strings.Repeat("x", 500)}}

	if _, err := tr.Apply(ctx, msgs, 100); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if gw.calls != 1 {
		t.Fatalf("want one summary call, got %d", gw.calls)
	}
	if gw.taskID != "task-7" {
		t.Errorf("summary TaskID = %q, want %q", gw.taskID, "task-7")
	}
}
