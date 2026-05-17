package routing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func TestGenerateText_trimsResponse(t *testing.T) {
	router := NewRouter(&mockProvider{providerName: "openai", budget: 10000})
	text, err := router.GenerateText(context.Background(), "hello", 100)
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if text != "ok" {
		t.Fatalf("text = %q", text)
	}
}

func TestGenerateStructuredJSON_nilTarget(t *testing.T) {
	router := NewRouter(&mockProvider{providerName: "openai", budget: 10000})
	err := router.GenerateStructuredJSON(context.Background(), "prompt", nil)
	if err == nil || !strings.Contains(err.Error(), "target is required") {
		t.Fatalf("GenerateStructuredJSON() error = %v", err)
	}
}

func TestGenerateStructuredJSON_success(t *testing.T) {
	router := NewRouter(&jsonResponseProvider{
		providerName: "openai",
		content:      `{"name":"agentd"}`,
	})
	var out struct {
		Name string `json:"name"`
	}
	if err := router.GenerateStructuredJSON(context.Background(), "prompt", &out); err != nil {
		t.Fatalf("GenerateStructuredJSON() error = %v", err)
	}
	if out.Name != "agentd" {
		t.Fatalf("out = %+v", out)
	}
}

type invalidPayload struct {
	Value int `json:"value"`
}

func (invalidPayload) Validate() error {
	return models.ErrInvalidJSONResponse
}

func TestGenerateStructuredJSON_validationFailure(t *testing.T) {
	router := NewRouter(&jsonResponseProvider{
		providerName: "openai",
		content:      `{"value":1}`,
	})
	var out invalidPayload
	err := router.GenerateStructuredJSON(context.Background(), "prompt", &out)
	if err == nil {
		t.Fatal("GenerateStructuredJSON() error = nil, want validation error")
	}
}

func TestTruncateToBudget(t *testing.T) {
	router := NewRouter()
	input := strings.Repeat("x", 100)
	if got := router.TruncateToBudget(input, 0); got != input {
		t.Fatalf("maxTokens=0: got len %d, want %d", len(got), len(input))
	}
	if got := router.TruncateToBudget(input, 10); len(got) != 40 {
		t.Fatalf("truncated len = %d, want 40", len(got))
	}
	if got := router.TruncateToBudget("short", 10); got != "short" {
		t.Fatalf("short input changed: %q", got)
	}
}

type errorTruncator struct{}

func (errorTruncator) Apply(context.Context, []spec.PromptMessage, int) ([]spec.PromptMessage, error) {
	return nil, spec.ErrContextBudgetExceeded
}

func TestGenerate_truncationErrorDoesNotCascade(t *testing.T) {
	p := &callCountProvider{mockProvider: mockProvider{providerName: "openai", budget: 10000}}
	router := NewRouter(p, &mockProvider{providerName: "ollama", budget: 10000}).
		WithTruncation(errorTruncator{}, 12000)

	_, err := router.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, spec.ErrContextBudgetExceeded) {
		t.Fatalf("Generate() error = %v", err)
	}
	if p.calls != 0 {
		t.Fatal("provider Generate was called after truncation error")
	}
}

type callCountProvider struct {
	mockProvider
	calls int
}

func (p *callCountProvider) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	p.calls++
	return p.mockProvider.Generate(ctx, req)
}

func TestGenerate_noProviders(t *testing.T) {
	router := NewRouter()
	_, err := router.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no LLM providers configured") {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestGenerate_explicitProviderNotConfigured(t *testing.T) {
	router := NewRouter(&mockProvider{providerName: "openai", budget: 10000})
	_, err := router.Generate(context.Background(), spec.AIRequest{
		Provider: "missing",
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), `not configured`) {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestGenerate_skipTruncation(t *testing.T) {
	long := strings.Repeat("a", 500)
	p := &mockProvider{providerName: "openai", budget: 10}
	router := NewRouter(p).WithTruncation(errorTruncator{}, 12000)
	_, err := router.Generate(context.Background(), spec.AIRequest{
		Messages:       []spec.PromptMessage{{Role: "user", Content: long}},
		SkipTruncation: true,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if p.request.Messages[0].Content != long {
		t.Fatalf("message was truncated")
	}
}
