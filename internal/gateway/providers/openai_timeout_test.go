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
