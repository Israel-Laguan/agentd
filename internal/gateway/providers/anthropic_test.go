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
			Content: []anthropicContentBlock{
				{Type: "text", Text: stringPtr("hello from claude")},
			},
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

func TestAnthropicGenerate_CustomProviderNameInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: stringPtr("ok")},
			},
			Model: "claude-3-haiku",
		})
	}))
	defer srv.Close()

	a := NewAnthropic(spec.ProviderConfig{
		Name:    "my-anthropic",
		Adapter: "anthropic",
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		Model:   "claude-3-haiku",
	}, srv.Client())

	resp, err := a.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "my-anthropic" {
		t.Fatalf("ProviderUsed = %q, want my-anthropic", resp.ProviderUsed)
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

func newAnthropicToolCallsTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "tool_use", ToolUse: &anthropicToolUseBlock{
					ID: "call_abc123", Name: "get_weather",
					Input: map[string]interface{}{"location": "Boston", "unit": "celsius"},
				}},
				{Type: "tool_use", ToolUse: &anthropicToolUseBlock{
					ID: "call_xyz789", Name: "get_time",
					Input: map[string]interface{}{"timezone": "UTC"},
				}},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 10, OutputTokens: 140},
			Model: "claude-3-haiku",
		})
	}))
}

func TestAnthropicToolCalls_ParsesToolCalls(t *testing.T) {
	srv := newAnthropicToolCallsTestServer()
	defer srv.Close()
	a := NewAnthropic(spec.ProviderConfig{BaseURL: srv.URL, Model: "claude-3-haiku"}, srv.Client())
	resp, err := a.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "What's the weather and time?"}},
		Tools:    []spec.ToolDefinition{{Name: "get_weather", Description: "Get weather", Parameters: &spec.FunctionParameters{}}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	assertAnthropicParsedToolCalls(t, resp)
}

func assertAnthropicParsedToolCalls(t *testing.T, resp spec.AIResponse) {
	t.Helper()
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("ToolCalls length = %d, want 2", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_abc123" || resp.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("ToolCalls[0] = %+v", resp.ToolCalls[0])
	}
	assertJSONArgs(t, resp.ToolCalls[0].Function.Arguments, `{"location":"Boston","unit":"celsius"}`)
	if resp.ToolCalls[1].ID != "call_xyz789" || resp.ToolCalls[1].Function.Name != "get_time" {
		t.Errorf("ToolCalls[1] = %+v", resp.ToolCalls[1])
	}
	assertJSONArgs(t, resp.ToolCalls[1].Function.Arguments, `{"timezone":"UTC"}`)
}

func assertJSONArgs(t *testing.T, gotJSON, wantJSON string) {
	t.Helper()
	var got, want map[string]interface{}
	if err := json.Unmarshal([]byte(gotJSON), &got); err != nil {
		t.Errorf("unmarshal got: %v", err)
		return
	}
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("arg %q = %v, want %v", k, got[k], v)
		}
	}
}

func TestAnthropicToolCalls_WithTextContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{
				{
					Type: "text",
					Text: stringPtr("Hello, world!"),
				},
			},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 5, OutputTokens: 10},
			Model: "claude-3-haiku",
		})
	}))
	defer srv.Close()

	a := NewAnthropic(spec.ProviderConfig{
		BaseURL: srv.URL,
		Model:   "claude-3-haiku",
	}, srv.Client())

	resp, err := a.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	if resp.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello, world!")
	}
	if resp.ToolCalls != nil {
		t.Errorf("ToolCalls = %v, want nil", resp.ToolCalls)
	}
}

func newAnthropicToolsTestServer(t *testing.T, receivedBody *anthropicRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(receivedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{{Type: "text", Text: stringPtr("result")}},
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 5, OutputTokens: 5},
			Model: "claude-3-haiku",
		})
	}))
}

func TestAnthropicTools_Serialization(t *testing.T) {
	var receivedBody anthropicRequest
	srv := newAnthropicToolsTestServer(t, &receivedBody)
	defer srv.Close()

	a := NewAnthropic(spec.ProviderConfig{BaseURL: srv.URL, Model: "claude-3-haiku"}, srv.Client())

	_, err := a.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "What's the weather?"}},
		Tools: []spec.ToolDefinition{{
			Name:        "get_weather",
			Description: "Get weather for a location",
			Parameters: &spec.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"location": map[string]string{"type": "string"},
				},
				Required: []string{"location"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	if len(receivedBody.Tools) != 1 {
		t.Fatalf("Tools length = %d, want 1", len(receivedBody.Tools))
	}
	if receivedBody.Tools[0].Name != "get_weather" {
		t.Errorf("Tool name = %q, want %q", receivedBody.Tools[0].Name, "get_weather")
	}
	if receivedBody.Tools[0].Description != "Get weather for a location" {
		t.Errorf("Tool description = %q, want %q", receivedBody.Tools[0].Description, "Get weather for a location")
	}
	// Verify InputSchema is properly serialized
	if receivedBody.Tools[0].InputSchema == nil {
		t.Error("Tool InputSchema should not be nil")
	}
	if receivedBody.Tools[0].InputSchema.Type != "object" {
		t.Errorf("Tool InputSchema.Type = %q, want %q", receivedBody.Tools[0].InputSchema.Type, "object")
	}
	if receivedBody.Tools[0].InputSchema.Properties == nil {
		t.Error("Tool InputSchema.Properties should not be nil")
	}
	if _, ok := receivedBody.Tools[0].InputSchema.Properties["location"]; !ok {
		t.Error("Tool InputSchema.Properties should have 'location'")
	}
}

func TestAnthropicMaxTokens(t *testing.T) {
	t.Parallel()

	if got := anthropicMaxTokens(512); got != 512 {
		t.Fatalf("anthropicMaxTokens(512) = %d, want 512", got)
	}
	if got := anthropicMaxTokens(0); got != 1024 {
		t.Fatalf("anthropicMaxTokens(0) = %d, want 1024", got)
	}
}

func TestAnthropicCapabilities(t *testing.T) {
	a := NewAnthropic(spec.ProviderConfig{
		Model: "claude-3-haiku",
	}, nil)

	caps := a.Capabilities()
	if caps.SupportsChatTools != true {
		t.Fatalf("SupportsChatTools = %v, want true", caps.SupportsChatTools)
	}
}

func stringPtr(s string) *string {
	return &s
}
