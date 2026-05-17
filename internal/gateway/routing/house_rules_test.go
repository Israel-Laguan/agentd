package routing

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
)

func TestMergeHouseRulesIntoMessages_noSystemMessage(t *testing.T) {
	rules := "Use tabs."
	msgs := []spec.PromptMessage{{Role: "user", Content: "Hi"}}
	out := mergeHouseRulesIntoMessages(msgs, rules)
	if len(out) != 2 || out[0].Role != "system" {
		t.Fatalf("messages = %#v", out)
	}
	if !strings.Contains(out[0].Content, rules) {
		t.Fatalf("system = %q", out[0].Content)
	}
}

func TestWithHouseRules_emptyNoOp(t *testing.T) {
	ctx := WithHouseRules(context.Background(), "  ")
	if HouseRulesFromContext(ctx) != "" {
		t.Fatal("expected empty house rules")
	}
}

func TestRouterGenerateJSONInjectsHouseRules(t *testing.T) {
	p := &captureHouseRulesProvider{
		providerName: "openai",
		resp:         spec.AIResponse{Content: `{"intent":"ambiguous","reason":"x"}`, ProviderUsed: "openai"},
	}
	router := NewRouter(p)
	ctx := WithHouseRules(context.Background(), "POSIX sh only.")
	_, err := router.ClassifyIntent(ctx, "hello")
	if err != nil {
		t.Fatalf("ClassifyIntent: %v", err)
	}
	if p.lastReq == nil || len(p.lastReq.Messages) == 0 {
		t.Fatalf("house rules missing from JSON flow: %#v", p.lastReq)
	}
	if !strings.Contains(p.lastReq.Messages[0].Content, "POSIX sh only.") {
		t.Fatalf("house rules missing from JSON flow: %#v", p.lastReq)
	}
}

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

func (f *captureHouseRulesProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}
