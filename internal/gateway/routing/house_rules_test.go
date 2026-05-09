package routing

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestMergeHouseRulesIntoMessagesPrependsToSystem(t *testing.T) {
	rules := "Use tabs; never sudo."
	msgs := []spec.PromptMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hi"},
	}
	out := mergeHouseRulesIntoMessages(msgs, rules)
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	if !strings.Contains(out[0].Content, rules) || !strings.Contains(out[0].Content, "You are helpful.") {
		t.Fatalf("system = %q", out[0].Content)
	}
}

func TestRouterGenerateInjectsHouseRulesFromContext(t *testing.T) {
	p := &captureHouseRulesProvider{
		providerName: "openai",
		resp:         spec.AIResponse{Content: "ok", ProviderUsed: "openai"},
	}
	router := NewRouter(p)
	ctx := WithHouseRules(context.Background(), "Always use POSIX sh.")
	_, err := router.Generate(ctx, spec.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: "base"},
			{Role: "user", Content: "go"},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if p.lastReq == nil || len(p.lastReq.Messages) < 1 {
		t.Fatal("expected captured request messages")
	}
	if !strings.Contains(p.lastReq.Messages[0].Content, "Always use POSIX sh.") {
		t.Fatalf("system missing house rules: %q", p.lastReq.Messages[0].Content)
	}
}

type captureHouseRulesProvider struct {
	providerName string
	resp         spec.AIResponse
	err          error
	calls        int
	lastReq      *spec.AIRequest
}

func (f *captureHouseRulesProvider) Name() spec.Provider { return spec.Provider(f.providerName) }

func (f *captureHouseRulesProvider) MaxInputChars() int { return 100000 }

func (f *captureHouseRulesProvider) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	f.calls++
	cp := req
	f.lastReq = &cp
	if f.err != nil {
		return spec.AIResponse{}, f.err
	}
	return f.resp, nil
}
