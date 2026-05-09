package routing

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

func TestRouterUsesProviderSpecificBudget(t *testing.T) {
	provider := &captureProvider{providerName: "openai", budget: 10}
	router := NewRouter(provider).WithTruncation(truncation.StrategyTruncator{Strategy: truncation.MiddleOutStrategy{}}, 50)
	if _, err := router.Generate(context.Background(), spec.AIRequest{Messages: []spec.PromptMessage{{Role: "user", Content: strings.Repeat("a", 100)}}}); err != nil {
		t.Fatal(err)
	}
	if got := len(provider.request.Messages[0].Content); got != 10 {
		t.Fatalf("message len = %d, want provider budget 10", got)
	}
}

type captureProvider struct {
	providerName string
	budget       int
	request      spec.AIRequest
}

func (p *captureProvider) Name() spec.Provider { return spec.Provider(p.providerName) }
func (p *captureProvider) MaxInputChars() int   { return p.budget }
func (p *captureProvider) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	p.request = req
	return spec.AIResponse{Content: "ok", ProviderUsed: string(p.providerName)}, nil
}
