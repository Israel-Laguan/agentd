package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// TestAgenticLoop_ContextWarningSummarize verifies preemptive summarization at the
// warning threshold before the hard character budget is hit.
func TestAgenticLoop_ContextWarningSummarize(t *testing.T) {
	t.Parallel()
	track := &summarizeTrackingGateway{}
	cm := NewContextManager(config.AgenticContextConfig{
		AnchorBudget:          100,
		WorkingBudget:         200,
		CompressedBudget:      100,
		RollingThresholdTurns: 100,
		KeepRecentTurns:       1,
	}, track, "agent", "task-ctx")

	var b strings.Builder
	for b.Len() < 300 {
		b.WriteString("x")
	}
	big := b.String()
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: big},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: big},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: big},
	}

	ctxBudget := NewContextBudgetGuard(400, 0.5)
	if warn, _ := ctxBudget.Check(totalChars(messages)); !warn {
		t.Fatalf("expected warn at %d chars", totalChars(messages))
	}
	if _, err := cm.PrepareContextForceSummarize(context.Background(), messages); err != nil {
		t.Fatalf("PrepareContextForceSummarize: %v", err)
	}
	if !track.summarized {
		t.Fatal("expected force summarization at context warning threshold")
	}
}

type summarizeTrackingGateway struct {
	summarized bool
}

func (g *summarizeTrackingGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if req.JSONMode {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "Summarize the following") {
				g.summarized = true
				return gateway.AIResponse{Content: `{"decisions_made":["d"],"facts_established":["f"]}`}, nil
			}
		}
	}
	return gateway.AIResponse{Content: "ok"}, nil
}

func (g *summarizeTrackingGateway) GeneratePlan(ctx context.Context, s string) (*models.DraftPlan, error) {
	return &models.DraftPlan{}, nil
}

func (g *summarizeTrackingGateway) AnalyzeScope(ctx context.Context, s string) (*spec.ScopeAnalysis, error) {
	return nil, nil
}

func (g *summarizeTrackingGateway) ClassifyIntent(ctx context.Context, s string) (*spec.IntentAnalysis, error) {
	return nil, nil
}
func (g *summarizeTrackingGateway) Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.NoopEmbed(ctx, req)
}

