package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestAnthropicGenerate(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody anthropicRequest
	srv := newAnthropicTestServer(t, &receivedHeaders, &receivedBody)
	defer srv.Close()

	a := NewAnthropic(spec.ProviderConfig{
		BaseURL: srv.URL,
		APIKey:  "sk-test-key",
		Model:   "claude-3-haiku",
	}, srv.Client())

	resp, err := a.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Say hello"},
		},
		Temperature: 0.3,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	assertAnthropicRequestHeaders(t, receivedHeaders)
	assertAnthropicRequestBody(t, receivedBody)
	assertAnthropicResponse(t, resp)
}

func newAnthropicTestServer(t *testing.T, receivedHeaders *http.Header, receivedBody *anthropicRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*receivedHeaders = r.Header
		if err := json.NewDecoder(r.Body).Decode(receivedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{Type: "text", Text: "hello from claude"}},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 10, OutputTokens: 5},
			Model: "claude-3-haiku",
		})
	}))
}

func assertAnthropicRequestHeaders(t *testing.T, receivedHeaders http.Header) {
	t.Helper()
	if receivedHeaders.Get("x-api-key") != "sk-test-key" {
		t.Fatalf("x-api-key = %q", receivedHeaders.Get("x-api-key"))
	}
	if receivedHeaders.Get("anthropic-version") != anthropicAPIVersion {
		t.Fatalf("anthropic-version = %q", receivedHeaders.Get("anthropic-version"))
	}
}

func assertAnthropicRequestBody(t *testing.T, receivedBody anthropicRequest) {
	t.Helper()
	if receivedBody.System != "You are helpful." {
		t.Fatalf("system = %q, want flattened system content", receivedBody.System)
	}
	if len(receivedBody.Messages) != 1 || receivedBody.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v, want single user message", receivedBody.Messages)
	}
}

func assertAnthropicResponse(t *testing.T, resp spec.AIResponse) {
	t.Helper()
	if resp.Content != "hello from claude" {
		t.Fatalf("Content = %q", resp.Content)
	}
	if resp.TokenUsage != 15 {
		t.Fatalf("TokenUsage = %d, want 15", resp.TokenUsage)
	}
	if resp.ProviderUsed != "anthropic" {
		t.Fatalf("ProviderUsed = %q", resp.ProviderUsed)
	}
	if resp.ModelUsed != "claude-3-haiku" {
		t.Fatalf("ModelUsed = %q", resp.ModelUsed)
	}
}

func TestAnthropicSystemMessageFlattening(t *testing.T) {
	messages := []spec.PromptMessage{
		{Role: "system", Content: "First system."},
		{Role: "system", Content: "Second system."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
		{Role: "user", Content: "Bye"},
	}
	system, out := splitSystemMessages(messages)
	if system != "First system.\nSecond system." {
		t.Fatalf("system = %q", system)
	}
	if len(out) != 3 {
		t.Fatalf("messages len = %d, want 3", len(out))
	}
	if out[0].Role != "user" || out[1].Role != "assistant" || out[2].Role != "user" {
		t.Fatalf("roles = %v", out)
	}
}
