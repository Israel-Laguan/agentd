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
		Messages: []spec.PromptMessage{{Role: "user", Content: "What's the weather?"}},
		JSONMode: true,
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
		Messages: []spec.PromptMessage{{Role: "user", Content: "Return JSON"}},
		JSONMode: true,
		Tools:    nil,
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
}

func newOpenAIToolCallsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOpenAIJSON(t, w, map[string]any{
			"model": "gpt-test",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role": "assistant", "content": nil,
					"tool_calls": []map[string]any{
						{"id": "call_abc123", "type": "function", "function": map[string]any{
							"name": "get_weather", "arguments": `{"location":"Boston","unit":"celsius"}`,
						}},
						{"id": "call_xyz789", "type": "function", "function": map[string]any{
							"name": "get_time", "arguments": `{"timezone":"UTC"}`,
						}},
					},
				},
			}},
			"usage": map[string]int{"total_tokens": 150},
		})
	}))
}

func TestOpenAIToolCalls_ParsesToolCalls(t *testing.T) {
	srv := newOpenAIToolCallsTestServer(t)
	defer srv.Close()
	o := NewOpenAI(spec.ProviderConfig{BaseURL: srv.URL + "/v1", Model: "gpt-test"}, srv.Client())
	resp, err := o.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "What's the weather and time?"}},
		Tools:    []spec.ToolDefinition{{Name: "get_weather", Description: "Get weather", Parameters: &spec.FunctionParameters{}}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	assertOpenAIParsedToolCalls(t, resp)
}

func assertOpenAIParsedToolCalls(t *testing.T, resp spec.AIResponse) {
	t.Helper()
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("ToolCalls length = %d, want 2", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_abc123" || resp.ToolCalls[0].Type != "function" ||
		resp.ToolCalls[0].Function.Name != "get_weather" ||
		resp.ToolCalls[0].Function.Arguments != `{"location":"Boston","unit":"celsius"}` {
		t.Errorf("ToolCalls[0] = %+v", resp.ToolCalls[0])
	}
	if resp.ToolCalls[1].ID != "call_xyz789" || resp.ToolCalls[1].Type != "function" ||
		resp.ToolCalls[1].Function.Name != "get_time" ||
		resp.ToolCalls[1].Function.Arguments != `{"timezone":"UTC"}` {
		t.Errorf("ToolCalls[1] = %+v", resp.ToolCalls[1])
	}
}

func openAIToolConversationMessages() []spec.PromptMessage {
	return []spec.PromptMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "What's the weather?"},
		{Role: "assistant", ToolCalls: []spec.ToolCall{{ID: "call_abc", Type: "function", Function: spec.ToolCallFunction{
			Name: "get_weather", Arguments: `{"location":"Boston"}`,
		}}}},
		{Role: "tool", ToolCallID: "call_abc", Content: `{"temp":72,"conditions":"sunny"}`},
		{Role: "assistant", Content: "It's sunny and 72°F in Boston."},
	}
}

func TestOpenAIRequest_ToolConversationOmitsAssistantContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		messages, ok := reqBody["messages"].([]any)
		if !ok || len(messages) != 5 {
			t.Fatalf("messages = %v, want 5 messages", reqBody["messages"])
		}
		assistant, ok := messages[2].(map[string]any)
		if !ok {
			t.Fatalf("messages[2] is not an object: %T", messages[2])
		}
		if assistant["role"] != "assistant" {
			t.Errorf("messages[2].role = %v, want assistant", assistant["role"])
		}
		if content, hasContent := assistant["content"]; hasContent && content != nil {
			t.Errorf("assistant with tool_calls should omit content or use null, got %v", content)
		}
		tc, ok := assistant["tool_calls"].([]any)
		if !ok || len(tc) != 1 {
			t.Fatalf("tool_calls = %v", assistant["tool_calls"])
		}
		toolMsg, ok := messages[3].(map[string]any)
		if !ok {
			t.Fatalf("messages[3] is not an object: %T", messages[3])
		}
		if toolMsg["role"] != "tool" {
			t.Errorf("messages[3].role = %v, want tool", toolMsg["role"])
		}
		if toolMsg["tool_call_id"] != "call_abc" {
			t.Errorf("tool_call_id = %v, want call_abc", toolMsg["tool_call_id"])
		}
		if toolMsg["content"] != `{"temp":72,"conditions":"sunny"}` {
			t.Errorf("tool content = %v", toolMsg["content"])
		}
		writeOpenAIJSON(t, w, openAIResponseBody("done", "gpt-test"))
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
	}, srv.Client())

	_, err := o.Generate(context.Background(), spec.AIRequest{
		Messages: openAIToolConversationMessages(),
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
}

func TestOpenAIResponse_UnmarshalAssistantToolCallsSnippet(t *testing.T) {
	raw := `{
		"model": "gpt-4",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [{
					"id": "call_abc123",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"location\":\"Boston\"}"
					}
				}]
			}
		}],
		"usage": {"total_tokens": 42}
	}`
	var decoded openAIResponse
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(decoded.Choices) != 1 {
		t.Fatalf("len(choices) = %d, want 1", len(decoded.Choices))
	}
	msg := decoded.Choices[0].Message
	if msg.Role != "assistant" {
		t.Errorf("role = %q, want assistant", msg.Role)
	}
	if msg.Content != nil {
		t.Errorf("content = %v, want nil", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("len(tool_calls) = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_abc123" || tc.Type != "function" ||
		tc.Function.Name != "get_weather" ||
		tc.Function.Arguments != `{"location":"Boston"}` {
		t.Errorf("tool_call = %+v", tc)
	}
	resp := decoded.toAIResponse("gpt-4")
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_abc123" {
		t.Errorf("toAIResponse ToolCalls = %+v", resp.ToolCalls)
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

// TestOpenAICapabilities_SupportsChatTools verifies that OpenAI provider's
// Capabilities() returns SupportsChatTools = true.
// Validates: Requirements 1.3, 5.1, 5.5
func TestOpenAICapabilities_SupportsChatTools(t *testing.T) {
	t.Parallel()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4",
	}, nil)

	caps := o.Capabilities()
	if !caps.SupportsChatTools {
		t.Errorf("Capabilities().SupportsChatTools = false, want true")
	}
}

// TestOpenAICapabilities_Consistency verifies that the Capabilities result
// is consistent across multiple calls (idempotent query).
// Validates: Property 1 - Capability Consistency
func TestOpenAICapabilities_Consistency(t *testing.T) {
	t.Parallel()

	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4",
	}, nil)

	// Call Capabilities multiple times and verify consistency
	caps1 := o.Capabilities()
	caps2 := o.Capabilities()
	caps3 := o.Capabilities()

	if caps1 != caps2 || caps2 != caps3 {
		t.Errorf("Capabilities() returned inconsistent results: call1=%+v call2=%+v call3=%+v", caps1, caps2, caps3)
	}
}
