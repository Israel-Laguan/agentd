package truncation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func TestStrategyTruncatorPolicies(t *testing.T) {
	msgs := []spec.PromptMessage{{Role: "user", Content: strings.Repeat("a", 50) + strings.Repeat("z", 50)}}

	middle, err := NewTruncator(TruncatorPolicyMiddleOut, 0.5, nil, nil).Apply(context.Background(), msgs, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(middle[0].Content, TruncationMarker) {
		t.Fatalf("middle_out did not truncate with marker: %q", middle[0].Content)
	}

	headTail, err := NewTruncator(TruncatorPolicyHeadTail, 1, nil, nil).Apply(context.Background(), msgs, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(headTail[0].Content, strings.Repeat("a", 10)) {
		t.Fatalf("head_tail ratio did not keep head: %q", headTail[0].Content)
	}
}

func TestRejectTruncatorReturnsBudgetError(t *testing.T) {
	_, err := RejectTruncator{}.Apply(context.Background(), []spec.PromptMessage{{Role: "user", Content: "too long"}}, 3)
	if !errors.Is(err, spec.ErrContextBudgetExceeded) {
		t.Fatalf("err = %v, want ErrContextBudgetExceeded", err)
	}
}

func TestSummarizeTruncatorUsesGateway(t *testing.T) {
	gw := &summaryGateway{content: "short summary"}
	msgs, err := SummarizeTruncator{Gateway: gw, Fallback: MiddleOutStrategy{}}.Apply(context.Background(), []spec.PromptMessage{{Role: "user", Content: strings.Repeat("a", 100)}}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if msgs[0].Content != "short summary" {
		t.Fatalf("content = %q, want summary", msgs[0].Content)
	}
	if !gw.skipTruncation {
		t.Fatal("summary request did not set SkipTruncation")
	}
}

func TestSummarizeTruncatorFallsBackWhenBreakerOpen(t *testing.T) {
	gw := &summaryGateway{content: "short summary"}
	msgs, err := SummarizeTruncator{Gateway: gw, Breaker: openBreaker{}, Fallback: MiddleOutStrategy{}}.Apply(context.Background(), []spec.PromptMessage{{Role: "user", Content: strings.Repeat("a", 100)}}, 40)
	if err != nil {
		t.Fatal(err)
	}
	if gw.calls != 0 {
		t.Fatalf("gateway calls = %d, want 0", gw.calls)
	}
	if !strings.Contains(msgs[0].Content, TruncationMarker) {
		t.Fatalf("fallback did not middle-out truncate: %q", msgs[0].Content)
	}
}

type summaryGateway struct {
	content        string
	calls          int
	skipTruncation bool
}

func (g *summaryGateway) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	g.calls++
	g.skipTruncation = req.SkipTruncation
	return spec.AIResponse{Content: g.content}, nil
}

func (g *summaryGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *summaryGateway) AnalyzeScope(context.Context, string) (*spec.ScopeAnalysis, error) {
	return nil, nil
}

func (g *summaryGateway) ClassifyIntent(context.Context, string) (*spec.IntentAnalysis, error) {
	return nil, nil
}

type openBreaker struct{}

func (openBreaker) IsOpen() bool { return true }
