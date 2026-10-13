package routing

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

type capturingProvider struct {
	jsonResponseProvider
	last spec.AIRequest
}

func (p *capturingProvider) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	p.last = req
	p.providerName = "openai"
	return p.jsonResponseProvider.Generate(ctx, req)
}

func TestClassifyIntent_PromptScopesStatusCheckToBoard(t *testing.T) {
	t.Parallel()
	prov := &capturingProvider{jsonResponseProvider: jsonResponseProvider{content: `{"intent":"plan_request","reason":"asks agent to find out"}`}}
	if _, err := NewRouter(prov).ClassifyIntent(context.Background(), "what os and hardware have this machine"); err != nil {
		t.Fatalf("ClassifyIntent() error = %v", err)
	}
	system := prov.last.Messages[0].Content
	for _, want := range []string{
		"board",
		"ONLY",
		"what os and hardware",
		"find out",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("intent prompt missing %q:\n%s", want, system)
		}
	}
	if !strings.Contains(strings.ToLower(system), "plan_request") {
		t.Fatalf("intent prompt must route find-out requests to plan_request:\n%s", system)
	}
}
