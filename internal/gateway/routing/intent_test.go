package routing

import (
	"context"
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
)

type jsonResponseProvider struct {
	providerName string
	content      string
}

func (p *jsonResponseProvider) Name() spec.Provider { return spec.Provider(p.providerName) }
func (p *jsonResponseProvider) MaxInputChars() int  { return 100000 }
func (p *jsonResponseProvider) Generate(_ context.Context, _ spec.AIRequest) (spec.AIResponse, error) {
	return spec.AIResponse{Content: p.content, ProviderUsed: p.providerName}, nil
}
func (p *jsonResponseProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}

func TestClassifyIntent_normalizesUnknownIntent(t *testing.T) {
	router := NewRouter(&jsonResponseProvider{
		providerName: "openai",
		content:      `{"intent":"not_a_real_intent","reason":"unclear"}`,
	})
	got, err := router.ClassifyIntent(context.Background(), "build me an api")
	if err != nil {
		t.Fatalf("ClassifyIntent() error = %v", err)
	}
	if got.Intent != "ambiguous" {
		t.Fatalf("intent = %q, want ambiguous", got.Intent)
	}
}

func TestClassifyIntent_validIntent(t *testing.T) {
	router := NewRouter(&jsonResponseProvider{
		providerName: "openai",
		content:      `{"intent":"plan_request","reason":"build app"}`,
	})
	got, err := router.ClassifyIntent(context.Background(), "build me an api")
	if err != nil {
		t.Fatalf("ClassifyIntent() error = %v", err)
	}
	if got.Intent != "plan_request" {
		t.Fatalf("intent = %q, want plan_request", got.Intent)
	}
}
