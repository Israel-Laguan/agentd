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

func TestOpenAITimeout_CancelsSlowRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeOpenAIJSON(t, w, openAIResponseBody("late", "gpt-test"))
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
		Timeout: 20 * time.Millisecond,
	}, srv.Client())

	_, err := o.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("error = %v, want ErrLLMUnreachable", err)
	}
}

func TestOpenAITimeout_ZeroDoesNotEnforceTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOpenAIJSON(t, w, openAIResponseBody("fast", "gpt-test"))
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
		Timeout: 0,
	}, srv.Client())

	resp, err := o.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Content != "fast" {
		t.Fatalf("Content = %q", resp.Content)
	}
}

func openAIResponseBody(content, model string) map[string]any {
	return map[string]any{
		"model": model,
		"choices": []map[string]any{{
			"message": spec.PromptMessage{Role: "assistant", Content: content},
		}},
		"usage": map[string]int{"total_tokens": 4},
	}
}

func writeOpenAIJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestOpenAITools_Serialization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools, ok := reqBody["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Fatal("expected tools in request body")
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatal("tool is not a map")
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			t.Fatal("function is not a map")
		}
		if fn["name"] != "get_weather" {
			t.Errorf("tool name = %q, want %q", fn["name"], "get_weather")
		}
		if fn["description"] != "Get weather for a location" {
			t.Errorf("tool description = %q, want %q", fn["description"], "Get weather for a location")
		}
		params, ok := fn["parameters"].(map[string]any)
		if !ok {
			t.Fatal("parameters is not a map")
		}
		if params["type"] != "object" {
			t.Errorf("params type = %q, want %q", params["type"], "object")
		}
		writeOpenAIJSON(t, w, openAIResponseBody("result", "gpt-test"))
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
	}, srv.Client())

	_, err := o.Generate(context.Background(), spec.AIRequest{
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
}

func TestOpenAITools_WithJSONMode_OmitsResponseFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := reqBody["response_format"]; ok {
			t.Error("expected no response_format when tools are present with JSONMode")
		}
		writeOpenAIJSON(t, w, openAIResponseBody("result", "gpt-test"))
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
	}, srv.Client())

	_, err := o.Generate(context.Background(), spec.AIRequest{
		Messages:  []spec.PromptMessage{{Role: "user", Content: "What's the weather?"}},
		JSONMode:  true,
		Tools: []spec.ToolDefinition{{
			Name:        "get_weather",
			Description: "Get weather for a location",
			Parameters:  &spec.FunctionParameters{},
		}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
}

func TestOpenAIJSONMode_WithoutTools_SetsResponseFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		rf, ok := reqBody["response_format"].(map[string]any)
		if !ok {
			t.Fatal("expected response_format when JSONMode true and no tools")
		}
		if rf["type"] != "json_object" {
			t.Errorf("response_format type = %q, want %q", rf["type"], "json_object")
		}
		writeOpenAIJSON(t, w, openAIResponseBody("{}", "gpt-test"))
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
	}, srv.Client())

	_, err := o.Generate(context.Background(), spec.AIRequest{
		Messages:  []spec.PromptMessage{{Role: "user", Content: "Return JSON"}},
		JSONMode:  true,
		Tools:     nil,
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
}

func TestOpenAIToolCalls_ParsesToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"model": "gpt-test",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]any{
						{
							"id":   "call_abc123",
							"type": "function",
							"function": map[string]any{
								"name":      "get_weather",
								"arguments": `{"location":"Boston","unit":"celsius"}`,
							},
						},
						{
							"id":   "call_xyz789",
							"type": "function",
							"function": map[string]any{
								"name":      "get_time",
								"arguments": `{"timezone":"UTC"}`,
							},
						},
					},
				},
			}},
			"usage": map[string]int{"total_tokens": 150},
		}
		writeOpenAIJSON(t, w, resp)
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
	}, srv.Client())

	resp, err := o.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "What's the weather and time?"}},
		Tools: []spec.ToolDefinition{{
			Name:        "get_weather",
			Description: "Get weather for a location",
			Parameters:  &spec.FunctionParameters{},
		}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("ToolCalls length = %d, want 2", len(resp.ToolCalls))
	}

	if resp.ToolCalls[0].ID != "call_abc123" {
		t.Errorf("ToolCalls[0].ID = %q, want %q", resp.ToolCalls[0].ID, "call_abc123")
	}
	if resp.ToolCalls[0].Type != "function" {
		t.Errorf("ToolCalls[0].Type = %q, want %q", resp.ToolCalls[0].Type, "function")
	}
	if resp.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("ToolCalls[0].Function.Name = %q, want %q", resp.ToolCalls[0].Function.Name, "get_weather")
	}
	if resp.ToolCalls[0].Function.Arguments != `{"location":"Boston","unit":"celsius"}` {
		t.Errorf("ToolCalls[0].Function.Arguments = %q, want %q", resp.ToolCalls[0].Function.Arguments, `{"location":"Boston","unit":"celsius"}`)
	}

	if resp.ToolCalls[1].ID != "call_xyz789" {
		t.Errorf("ToolCalls[1].ID = %q, want %q", resp.ToolCalls[1].ID, "call_xyz789")
	}
	if resp.ToolCalls[1].Function.Name != "get_time" {
		t.Errorf("ToolCalls[1].Function.Name = %q, want %q", resp.ToolCalls[1].Function.Name, "get_time")
	}
}

func TestOpenAIToolCalls_EmptyWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"model": "gpt-test",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello, world!",
				},
			}},
			"usage": map[string]int{"total_tokens": 10},
		}
		writeOpenAIJSON(t, w, resp)
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
	}, srv.Client())

	resp, err := o.Generate(context.Background(), spec.AIRequest{
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
