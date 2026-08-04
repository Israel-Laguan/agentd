// Package providers wire-contract tests declare the single wire contract
// (OpenAI Chat Completions) that both the direct and managed topologies
// depend on. See docs/llm-connector-strategy.md.
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// newWireContractServer returns an httptest.Server with the given handler,
// registered for cleanup.
func newWireContractServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// wireContractOpenAI returns an *OpenAI wired to the given fixture server.
func wireContractOpenAI(t *testing.T, srv *httptest.Server) *OpenAI {
	t.Helper()
	return NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "wire-test",
	}, srv.Client())
}

// TestWireContract_ErrorStatusMapping is the explicit error/HTTP-status
// dimension of the wire contract. 429→quota, 5xx→unreachable, 4xx→rejected.
// It may run standalone (top level) or as a subtest of TestOpenAIWireContract,
// so it does not call t.Parallel() itself; its subtests do.
func TestWireContract_ErrorStatusMapping(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantErrIs  error
		wantSubstr string
	}{
		{"429_maps_to_quota", http.StatusTooManyRequests, models.ErrLLMQuotaExceeded, ""},
		{"502_maps_to_unreachable", http.StatusBadGateway, models.ErrLLMUnreachable, "status 502"},
		{"400_maps_to_rejected", http.StatusBadRequest, nil, "provider rejected request: status 400"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newWireContractServer(t, func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", tc.status)
			})
			_, err := wireContractOpenAI(t, srv).Generate(context.Background(), spec.AIRequest{
				Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
				t.Fatalf("error = %v, want Is(%v)", err, tc.wantErrIs)
			}
			if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error msg = %q, want substring %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

// TestOpenAIWireContract exercises the 7-dimension wire-contract matrix
// against a shared httptest fixture. Each dimension lives in its own function
// so the matrix stays readable and within the per-function line budget.
func TestOpenAIWireContract(t *testing.T) {
	t.Parallel()
	t.Run("tools_request_shape", wireContractToolsRequestShape)
	t.Run("tool_calls_response", wireContractToolCallsResponse)
	t.Run("tool_call_id_round_trip", wireContractToolCallIDRoundTrip)
	t.Run("error_status_mapping", wireContractErrorStatusMapping)
	t.Run("timeout_behavior", wireContractTimeoutBehavior)
	t.Run("json_mode_x_tools", wireContractJSONModeXTools)
	t.Run("embeddings_passthrough", wireContractEmbeddingsPassthrough)
}

func wireContractToolsRequestShape(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := newWireContractServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeOpenAIJSON(t, w, openAIResponseBody("ok", "wire-test"))
	})
	_, err := wireContractOpenAI(t, srv).Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
		Tools: []spec.ToolDefinition{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters: &spec.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{"location": map[string]string{"type": "string"}},
				Required:   []string{"location"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatal("expected tools array")
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tools[0] type = %T", tools[0])
	}
	if tool["type"] != "function" {
		t.Errorf("tool type = %v, want function", tool["type"])
	}
	fn, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatalf("function type = %T", tool["function"])
	}
	if fn["name"] != "get_weather" {
		t.Errorf("tool name = %q, want get_weather", fn["name"])
	}
	params, ok := fn["parameters"].(map[string]any)
	if !ok {
		t.Fatal("parameters not a map")
	}
	if params["type"] != "object" {
		t.Errorf("params type = %q, want object", params["type"])
	}
}

func wireContractToolCallsResponse(t *testing.T) {
	t.Parallel()
	srv := newWireContractServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeOpenAIJSON(t, w, map[string]any{
			"model": "wire-test",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]any{{
						"id":   "call_1",
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
	})
	resp, err := wireContractOpenAI(t, srv).Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
		Tools:    []spec.ToolDefinition{{Name: "get_weather"}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" ||
		tc.Function.Name != "get_weather" ||
		tc.Function.Arguments != `{"location":"Boston"}` {
		t.Errorf("ToolCall = %+v", tc)
	}
}

func wireContractToolCallIDRoundTrip(t *testing.T) {
	t.Parallel()
	srv := newWireContractServer(t, func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		msgs, _ := reqBody["messages"].([]any)
		if len(msgs) != 3 {
			t.Fatalf("messages len = %d, want 3", len(msgs))
		}
		toolMsg, ok := msgs[2].(map[string]any)
		if !ok {
			t.Fatalf("messages[2] type = %T", msgs[2])
		}
		if toolMsg["role"] != "tool" {
			t.Errorf("role = %v, want tool", toolMsg["role"])
		}
		if toolMsg["tool_call_id"] != "call_abc" {
			t.Errorf("tool_call_id = %v, want call_abc", toolMsg["tool_call_id"])
		}
		if toolMsg["content"] != "result" {
			t.Errorf("content = %v, want result", toolMsg["content"])
		}
		writeOpenAIJSON(t, w, openAIResponseBody("done", "wire-test"))
	})
	_, err := wireContractOpenAI(t, srv).Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []spec.ToolCall{{
				ID: "call_abc", Type: "function",
				Function: spec.ToolCallFunction{Name: "get_weather", Arguments: "{}"},
			}}},
			{Role: "tool", ToolCallID: "call_abc", Content: "result"},
		},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
}

func wireContractErrorStatusMapping(t *testing.T) {
	t.Parallel()
	TestWireContract_ErrorStatusMapping(t)
}

func wireContractTimeoutBehavior(t *testing.T) {
	t.Parallel()
	srv := newWireContractServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeOpenAIJSON(t, w, openAIResponseBody("late", "wire-test"))
	})
	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "wire-test",
		Timeout: 50 * time.Millisecond,
	}, srv.Client())
	_, err := o.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("error = %v, want ErrLLMUnreachable", err)
	}
}

func wireContractJSONModeXTools(t *testing.T) {
	t.Parallel()
	t.Run("with_tools_omits_response_format", func(t *testing.T) {
		t.Parallel()
		var gotBody map[string]any
		srv := newWireContractServer(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			writeOpenAIJSON(t, w, openAIResponseBody("ok", "wire-test"))
		})
		_, err := wireContractOpenAI(t, srv).Generate(context.Background(), spec.AIRequest{
			Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
			JSONMode: true,
			Tools: []spec.ToolDefinition{{
				Name:       "get_weather",
				Parameters: &spec.FunctionParameters{},
			}},
		})
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		if _, has := gotBody["response_format"]; has {
			t.Error("expected no response_format when tools present with JSONMode")
		}
	})
	t.Run("without_tools_sets_response_format", func(t *testing.T) {
		t.Parallel()
		var gotBody map[string]any
		srv := newWireContractServer(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			writeOpenAIJSON(t, w, openAIResponseBody("{}", "wire-test"))
		})
		_, err := wireContractOpenAI(t, srv).Generate(context.Background(), spec.AIRequest{
			Messages: []spec.PromptMessage{{Role: "user", Content: "Return JSON"}},
			JSONMode: true,
		})
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		rf, ok := gotBody["response_format"].(map[string]any)
		if !ok {
			t.Fatal("expected response_format when JSONMode true and no tools")
		}
		if rf["type"] != "json_object" {
			t.Errorf("response_format type = %q, want json_object", rf["type"])
		}
	})
}

func wireContractEmbeddingsPassthrough(t *testing.T) {
	t.Parallel()
	srv := newWireContractServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(openAIEmbedResponse{
			Model: "text-embedding-3-small",
			Data: []openAIEmbedData{
				{Index: 0, Embedding: []float32{1, 0}},
				{Index: 2, Embedding: []float32{0, 1}},
			},
		})
	})
	o := NewOpenAI(spec.ProviderConfig{BaseURL: srv.URL, APIKey: "test"}, srv.Client())
	resp, err := o.Embed(context.Background(), spec.EmbedRequest{
		Input: []string{"a", "b", "c"},
	})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(resp.Vectors) != 3 {
		t.Fatalf("vectors len = %d, want 3", len(resp.Vectors))
	}
	if resp.Vectors[0] == nil || resp.Vectors[0][0] != 1 {
		t.Fatalf("vectors[0] = %v, want [1,0]", resp.Vectors[0])
	}
	if resp.Vectors[1] != nil {
		t.Fatalf("vectors[1] = %v, want nil", resp.Vectors[1])
	}
	if resp.Vectors[2] == nil || resp.Vectors[2][1] != 1 {
		t.Fatalf("vectors[2] = %v, want [0,1]", resp.Vectors[2])
	}
	if resp.ModelUsed != "text-embedding-3-small" {
		t.Fatalf("model used = %q", resp.ModelUsed)
	}
}
