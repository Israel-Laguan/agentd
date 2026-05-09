package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestAnthropicFallbackToRouter(t *testing.T) {
	openAI := &fakeProvider{providerName: "openai", err: http.ErrServerClosed}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "fallback ok"}},
			"model":   "claude-3-haiku",
		})
	}))
	defer srv.Close()

	anthropic := NewAnthropic(ProviderConfig{BaseURL: srv.URL, Model: "claude-3-haiku"}, srv.Client())
	router := NewRouter(openAI, anthropic)

	resp, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "anthropic" || resp.Content != "fallback ok" {
		t.Fatalf("response = %+v", resp)
	}
	if openAI.calls != 1 {
		t.Fatalf("openAI calls = %d, want 1", openAI.calls)
	}
}

func TestOpenAIGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		writeJSON(t, w, openAIResponseBody("hello", "gpt-test"))
	}))
	defer srv.Close()

	resp, err := NewOpenAI(ProviderConfig{BaseURL: srv.URL + "/v1", Model: "gpt-test"}, srv.Client()).
		Generate(context.Background(), sampleAIRequest())
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if resp.Content != "hello" || resp.ProviderUsed != "openai" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRouterFallbackToOllama(t *testing.T) {
	openAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "limited", http.StatusTooManyRequests)
	}))
	defer openAI.Close()
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{"message": PromptMessage{Content: "local"}, "model": "llama"})
	}))
	defer ollama.Close()

	router := NewRouter(
		NewOpenAI(ProviderConfig{BaseURL: openAI.URL, Model: "gpt"}, openAI.Client()),
		NewOllama(ProviderConfig{BaseURL: ollama.URL, Model: "llama"}, ollama.Client()),
	)
	req := sampleAIRequest()
	req.JSONMode = false
	resp, err := router.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "ollama" || resp.Content != "local" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRouterFallbackToOllamaWithInjectedProviders(t *testing.T) {
	openAI := &fakeProvider{providerName: "openai", err: fmt.Errorf("provider rejected request: status 401")}
	ollama := &fakeProvider{providerName: "ollama", resp: AIResponse{Content: "local", ProviderUsed: "ollama"}}

	resp, err := NewRouter(openAI, ollama).Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "generate"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if openAI.calls != 1 || ollama.calls != 1 {
		t.Fatalf("provider calls openai=%d ollama=%d", openAI.calls, ollama.calls)
	}
	if resp.ProviderUsed != "ollama" {
		t.Fatalf("ProviderUsed = %q, want ollama", resp.ProviderUsed)
	}
}

func TestRouterFallbackToHordeWithInjectedProviders(t *testing.T) {
	openAI := &fakeProvider{providerName: "openai", err: fmt.Errorf("provider rejected request: status 401")}
	ollama := &fakeProvider{providerName: "ollama", err: fmt.Errorf("provider rejected request: status 500")}
	horde := &fakeProvider{providerName: "horde", resp: AIResponse{Content: "crowd", ProviderUsed: "horde"}}

	resp, err := NewRouter(openAI, ollama, horde).Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "generate"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if openAI.calls != 1 || ollama.calls != 1 || horde.calls != 1 {
		t.Fatalf("provider calls openai=%d ollama=%d horde=%d", openAI.calls, ollama.calls, horde.calls)
	}
	if resp.ProviderUsed != "horde" || resp.Content != "crowd" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRouterFailsAfterAllThreeProvidersFail(t *testing.T) {
	openAI := &fakeProvider{providerName: "openai", err: fmt.Errorf("openai down")}
	ollama := &fakeProvider{providerName: "ollama", err: fmt.Errorf("ollama down")}
	horde := &fakeProvider{providerName: "horde", err: fmt.Errorf("horde down")}

	_, err := NewRouter(openAI, ollama, horde).Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "generate"}},
	})
	if err == nil {
		t.Fatalf("Generate() error = nil, want error")
	}
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("error = %v, want ErrLLMUnreachable", err)
	}
	for _, want := range []string{"openai down", "ollama down", "horde down"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestRouterJSONModeSelfCorrects(t *testing.T) {
	provider := &sequenceProvider{values: []string{`{"key": "value", }`, `{"key": "value"}`}}
	resp, err := NewRouter(provider).Generate(context.Background(), AIRequest{
		JSONMode: true,
		Messages: []PromptMessage{{Role: "user", Content: "json please"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.Content != `{"key": "value"}` {
		t.Fatalf("Content = %q", resp.Content)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(provider.requests))
	}
	if !strings.Contains(provider.requests[1].Messages[1].Content, "invalid JSON") {
		t.Fatalf("retry prompt = %#v", provider.requests[1].Messages)
	}
}

func TestRouterJSONModeFailsAfterRetriesWithRawOutput(t *testing.T) {
	raw := `{"key": "value", }`
	_, err := NewRouter(&sequenceProvider{values: []string{raw, raw, raw}}).
		Generate(context.Background(), AIRequest{JSONMode: true, Messages: []PromptMessage{{Role: "user", Content: "json"}}})
	if !errors.Is(err, models.ErrInvalidJSONResponse) {
		t.Fatalf("Generate() error = %v, want ErrInvalidJSONResponse", err)
	}
	if !strings.Contains(err.Error(), raw) {
		t.Fatalf("error %q does not include raw output %q", err.Error(), raw)
	}
}

func TestGenerateJSONSelfCorrects(t *testing.T) {
	gw := &sequenceGateway{values: []string{`{"ProjectName":`, `nope`, `{"ProjectName":"p","Tasks":[{"Title":"t1"}]}`}}
	got, err := GenerateJSON[models.DraftPlan](context.Background(), gw, sampleAIRequest())
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}
	if got.ProjectName != "p" || len(gw.requests[2].Messages) != 3 {
		t.Fatalf("plan=%#v requests=%#v", got, gw.requests)
	}
}

func TestGenerateJSONFailsAfterRetries(t *testing.T) {
	_, err := GenerateJSON[models.DraftPlan](context.Background(), &sequenceGateway{values: []string{"{", "{", "{"}}, sampleAIRequest())
	if !errors.Is(err, models.ErrInvalidJSONResponse) {
		t.Fatalf("GenerateJSON() error = %v", err)
	}
	if !strings.Contains(err.Error(), "{") {
		t.Fatalf("GenerateJSON() error %q missing raw output", err.Error())
	}
}

func sampleAIRequest() AIRequest {
	return AIRequest{Messages: []PromptMessage{{Role: "user", Content: "say hello"}}, JSONMode: true}
}

func openAIResponseBody(content, model string) map[string]any {
	return map[string]any{
		"model": model,
		"choices": []map[string]any{{
			"message": PromptMessage{Role: "assistant", Content: content},
		}},
		"usage": map[string]int{"total_tokens": 4},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

type sequenceGateway struct {
	values   []string
	requests []AIRequest
}

func (g *sequenceGateway) Generate(_ context.Context, req AIRequest) (AIResponse, error) {
	g.requests = append(g.requests, req)
	value := g.values[len(g.requests)-1]
	return AIResponse{Content: value}, nil
}

func (g *sequenceGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (*sequenceGateway) AnalyzeScope(context.Context, string) (*ScopeAnalysis, error) {
	return nil, nil
}

func (*sequenceGateway) ClassifyIntent(context.Context, string) (*IntentAnalysis, error) {
	return nil, nil
}

type fakeProvider struct {
	providerName string
	resp         AIResponse
	err          error
	calls        int
	budget       int
}

func (p *fakeProvider) Generate(context.Context, AIRequest) (AIResponse, error) {
	p.calls++
	if p.err != nil {
		return AIResponse{}, p.err
	}
	if p.resp.ProviderUsed == "" {
		p.resp.ProviderUsed = p.providerName
	}
	return p.resp, nil
}

func (p *fakeProvider) Name() Provider {
	return Provider(p.providerName)
}

func (p *fakeProvider) MaxInputChars() int {
	return p.budget
}

type sequenceProvider struct {
	values   []string
	requests []AIRequest
}

func (p *sequenceProvider) Generate(_ context.Context, req AIRequest) (AIResponse, error) {
	p.requests = append(p.requests, req)
	value := p.values[len(p.requests)-1]
	return AIResponse{Content: value, ProviderUsed: "mock"}, nil
}

func (*sequenceProvider) Name() Provider {
	return Provider("mock")
}

func (*sequenceProvider) MaxInputChars() int {
	return 0
}
