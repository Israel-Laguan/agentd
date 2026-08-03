package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func TestLlamaCpp_Generate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponseBody("hello from llamacpp", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL: srv.URL,
		Model:   "gpt-4",
	}, srv.Client())

	resp, err := l.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if resp.Content != "hello from llamacpp" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello from llamacpp")
	}
	if resp.ProviderUsed != "llamacpp" {
		t.Errorf("ProviderUsed = %q, want llamacpp", resp.ProviderUsed)
	}
}

func TestLlamaCpp_Timeout_CancelsSlowRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeOpenAIJSON(t, w, openAIResponseBody("late", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL: srv.URL,
		Model:   "gpt-4",
		Timeout: 20 * time.Millisecond,
	}, srv.Client())

	_, err := l.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("error = %v, want ErrLLMUnreachable", err)
	}
}

func TestLlamaCpp_Timeout_ZeroDoesNotEnforceTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOpenAIJSON(t, w, openAIResponseBody("fast", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL: srv.URL,
		Model:   "gpt-4",
		Timeout: 0,
	}, srv.Client())

	resp, err := l.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Content != "fast" {
		t.Fatalf("Content = %q", resp.Content)
	}
}

func TestLlamaCpp_Generate_SendsToolsWhenPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Fatal("expected tools array in request body")
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("tools[0] type = %T", tools[0])
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			t.Fatal("function is not a map")
		}
		if fn["name"] != "get_weather" {
			t.Errorf("tool name = %q, want get_weather", fn["name"])
		}
		if _, has := body["response_format"]; has {
			t.Error("expected no response_format when tools present")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponseBody("ok", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{BaseURL: srv.URL, Model: "gpt-4"}, srv.Client())
	_, err := l.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
		Tools: []spec.ToolDefinition{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters: &spec.FunctionParameters{
				Type: "object",
				Properties: map[string]any{"location": map[string]string{"type": "string"}},
				Required:   []string{"location"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
}

func TestLlamaCpp_Generate_ParsesToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "gpt-4",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]any{{
						"id":   "call_abc",
						"type": "function",
						"function": map[string]any{
							"name":      "get_weather",
							"arguments": `{"location":"Boston"}`,
						},
					}},
				},
			}},
			"usage": map[string]int{"total_tokens": 10},
		})
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{BaseURL: srv.URL, Model: "gpt-4"}, srv.Client())
	resp, err := l.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
		Tools: []spec.ToolDefinition{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters: &spec.FunctionParameters{Type: "object", Properties: map[string]any{}},
		}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want call_abc", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q, want get_weather", resp.ToolCalls[0].Function.Name)
	}
	if resp.ToolCalls[0].Function.Arguments != `{"location":"Boston"}` {
		t.Errorf("ToolCall.Arguments = %q, want {\"location\":\"Boston\"}", resp.ToolCalls[0].Function.Arguments)
	}
}

func TestLlamaCpp_ProbeTools_NoOpWhenDisabled(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponseBody("hi", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{BaseURL: srv.URL, Model: "gpt-4"}, srv.Client())
	if got := l.ProbeTools(context.Background()); got {
		t.Error("ProbeTools = true, want false when disabled")
	}
	if called {
		t.Error("probe made a request when disabled")
	}
}

func TestLlamaCpp_ProbeTools_Supported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "gpt-4",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]any{{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "get_weather",
							"arguments": "{}",
						},
					}},
				},
			}},
			"usage": map[string]int{"total_tokens": 5},
		})
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL:  srv.URL,
		Model:    "gpt-4",
		Options:  map[string]any{"probe_tools": true},
	}, srv.Client())
	if got := l.ProbeTools(context.Background()); !got {
		t.Error("ProbeTools = false, want true")
	}
	if l.Capabilities().SupportsChatTools != true {
		t.Error("Capabilities().SupportsChatTools = false after supported probe")
	}
}

func TestLlamaCpp_ProbeTools_NotSupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponseBody("I cannot do that", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL:  srv.URL,
		Model:    "gpt-4",
		Options:  map[string]any{"probe_tools": true},
	}, srv.Client())
	if got := l.ProbeTools(context.Background()); got {
		t.Error("ProbeTools = true, want false")
	}
	if l.Capabilities().SupportsChatTools != false {
		t.Error("Capabilities().SupportsChatTools = true after unsupported probe")
	}
}

func TestLlamaCpp_ProbeTools_NetworkError(t *testing.T) {
	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL:  "http://127.0.0.1:1",
		Model:    "gpt-4",
		Options:  map[string]any{"probe_tools": true},
	}, &http.Client{Timeout: 100 * time.Millisecond})
	if got := l.ProbeTools(context.Background()); got {
		t.Error("ProbeTools = true, want false on network error")
	}
	if l.Capabilities().SupportsChatTools != false {
		t.Error("Capabilities().SupportsChatTools = true after network error")
	}
}

func TestLlamaCpp_ProbeTools_Idempotent(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponseBody("hi", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL:  srv.URL,
		Model:    "gpt-4",
		Options:  map[string]any{"probe_tools": true},
	}, srv.Client())
	if got := l.ProbeTools(context.Background()); got != false {
		t.Errorf("ProbeTools = %v, want false", got)
	}
	if count != 1 {
		t.Fatalf("probe request count = %d, want 1", count)
	}
	if got := l.ProbeTools(context.Background()); got != false {
		t.Errorf("ProbeTools = %v, want false", got)
	}
	if count != 1 {
		t.Fatalf("probe request count = %d after second call, want 1", count)
	}
}
