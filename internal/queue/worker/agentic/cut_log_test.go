package agentic

import (
	"context"
	"strings"
	"testing"

	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestCutLogNamesOnlyLayersThatShrankTheInput(t *testing.T) {
	ctx, log := withCutLog(context.Background())
	noteCut(ctx, "working_zone", 100, 100)
	noteCut(ctx, "agentic_head", 100, 40)
	noteCut(ctx, "strategy_budget", 40, 10)
	if got := log.String(); got != "agentic_head,strategy_budget" {
		t.Fatalf("cut log = %q, want agentic_head,strategy_budget", got)
	}
	noteCut(context.Background(), "agentic_head", 5, 1) // no log in ctx: must not panic
}

type captureGateway struct {
	gateway.AIGateway
	req gateway.AIRequest
}

func (g *captureGateway) Generate(_ context.Context, r gateway.AIRequest) (gateway.AIResponse, error) {
	g.req = r
	return gateway.AIResponse{Content: "ok"}, nil
}

func TestGenerateTurnLabelsTheRequestWithTheLayerThatCutIt(t *testing.T) {
	gw := &captureGateway{}
	e := &Engine{config: Config{Gateway: gw, TruncatorMax: 30, CharacterBudget: 300}, host: &noopHost{}}
	msgs := []gateway.PromptMessage{
		{Role: "system", Content: "s"}, {Role: "user", Content: "task"},
		{Role: "assistant", Content: "c", ToolCalls: []gateway.ToolCall{{ID: "1", Function: gateway.ToolCallFunction{Name: "read"}}}},
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("x", 5000)},
	}
	ctx, _ := withCutLog(context.Background())
	_, _, err := e.generateAgenticTurn(ctx, models.Task{}, models.AgentProfile{}, &msgs, nil,
		agentruntime.NewBudgetGuard(nil, ""), agentruntime.NewContextBudgetGuard(1000, 0.8), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if gw.req.TruncatedBy == "" {
		t.Fatal("request carries no truncation layer although a 5000-char result was cut to a 300 budget")
	}
}
