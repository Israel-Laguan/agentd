package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func marshalOpenAIMessages(t *testing.T, msgs []spec.PromptMessage) []map[string]any {
	t.Helper()
	body, err := json.Marshal(openAIRequest{
		Model:    "gpt-4",
		Messages: messagesToOpenAI(msgs),
	})
	if err != nil {
		t.Fatalf("marshal openAIRequest: %v", err)
	}
	var decoded struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return decoded.Messages
}

func TestMessagesToOpenAI_AssistantWithToolCalls(t *testing.T) {
	t.Parallel()
	msgs := marshalOpenAIMessages(t, []spec.PromptMessage{
		{
			Role:    "assistant",
			Content: "I'll run it",
			ToolCalls: []spec.ToolCall{
				{
					ID:   "call_abc",
					Type: "function",
					Function: spec.ToolCallFunction{
						Name:      "bash",
						Arguments: `{"command":"pwd"}`,
					},
				},
			},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1", len(msgs))
	}
	msg := msgs[0]
	if msg["role"] != "assistant" {
		t.Fatalf("role = %v, want assistant", msg["role"])
	}
	if msg["content"] != "I'll run it" {
		t.Fatalf("content = %v, want I'll run it", msg["content"])
	}
	toolCalls, ok := msg["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("tool_calls = %v, want one entry", msg["tool_calls"])
	}
	tc, ok := toolCalls[0].(map[string]any)
	if !ok {
		t.Fatalf("tool_calls[0] type = %T", toolCalls[0])
	}
	if tc["id"] != "call_abc" {
		t.Fatalf("tool call id = %v, want call_abc", tc["id"])
	}
	fn, ok := tc["function"].(map[string]any)
	if !ok {
		t.Fatalf("function = %v", tc["function"])
	}
	if fn["name"] != "bash" {
		t.Fatalf("function name = %v, want bash", fn["name"])
	}
	if _, has := msg["tool_call_id"]; has {
		t.Fatal("assistant message must not include tool_call_id")
	}
}

func TestMessagesToOpenAI_ToolResult(t *testing.T) {
	t.Parallel()
	msgs := marshalOpenAIMessages(t, []spec.PromptMessage{
		{
			Role:       "tool",
			ToolCallID: "call_abc",
			Content:    "ok",
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1", len(msgs))
	}
	msg := msgs[0]
	if msg["role"] != "tool" {
		t.Fatalf("role = %v, want tool", msg["role"])
	}
	if msg["tool_call_id"] != "call_abc" {
		t.Fatalf("tool_call_id = %v, want call_abc", msg["tool_call_id"])
	}
	if msg["content"] != "ok" {
		t.Fatalf("content = %v, want ok", msg["content"])
	}
	if _, has := msg["tool_calls"]; has {
		t.Fatal("tool message must not include tool_calls")
	}
}

func TestMessagesToOpenAI_CompactUserMessage(t *testing.T) {
	t.Parallel()
	msgs := marshalOpenAIMessages(t, []spec.PromptMessage{
		{Role: "user", Content: "hi"},
	})
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1", len(msgs))
	}
	msg := msgs[0]
	for _, key := range []string{"tool_calls", "tool_call_id", "name"} {
		if _, has := msg[key]; has {
			t.Fatalf("compact user message must not include %q", key)
		}
	}
	if msg["role"] != "user" || msg["content"] != "hi" {
		t.Fatalf("message = %#v, want role user and content hi", msg)
	}
}

func TestMessagesToOpenAI_AssistantToolCallsOmitsEmptyContent(t *testing.T) {
	t.Parallel()
	msgs := marshalOpenAIMessages(t, []spec.PromptMessage{
		{
			Role: "assistant",
			ToolCalls: []spec.ToolCall{
				{ID: "call_1", Type: "function", Function: spec.ToolCallFunction{Name: "bash", Arguments: `{}`}},
			},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1", len(msgs))
	}
	msg := msgs[0]
	if _, has := msg["content"]; has {
		t.Fatal("assistant with tool_calls and empty content should omit content key")
	}
	if _, has := msg["tool_calls"]; !has {
		t.Fatal("expected tool_calls key")
	}
}

func TestMessagesToOpenAI_ToolResultEmptyContent(t *testing.T) {
	t.Parallel()
	msgs := marshalOpenAIMessages(t, []spec.PromptMessage{
		{
			Role:       "tool",
			ToolCallID: "call_abc",
			Content:    "",
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1", len(msgs))
	}
	msg := msgs[0]
	if msg["role"] != "tool" {
		t.Fatalf("role = %v, want tool", msg["role"])
	}
	if _, has := msg["content"]; !has {
		t.Fatal("tool message with empty content must include content key")
	}
	if msg["content"] != "" {
		t.Fatalf("content = %v, want empty string", msg["content"])
	}
}

func TestNewOpenAI_UnknownOptionLogsWarning(t *testing.T) {
	// Unrecognized options must produce a slog.Warn but must not cause any error
	// or prevent the provider from being constructed.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	old := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })

	o := NewOpenAI(spec.ProviderConfig{
		Options: map[string]any{"thinking_mode": true},
	}, nil)
	if o == nil {
		t.Fatal("NewOpenAI returned nil")
	}
	if !strings.Contains(buf.String(), "unknown provider option ignored") {
		t.Errorf("expected warning for unknown option; log = %q", buf.String())
	}
	if !strings.Contains(buf.String(), "thinking_mode") {
		t.Errorf("expected warning to name the key; log = %q", buf.String())
	}
}

// newOpenAICacheServer builds a test server that replies with the given usage
// payload map, so cache-token parsing can be exercised across provider shapes.
func newOpenAICacheServer(t *testing.T, usage map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"model": "gpt-test",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "cached",
				},
			}},
			"usage": usage,
		}
		writeOpenAIJSON(t, w, resp)
	}))
}

func openAIUsageResponse(t *testing.T, usage map[string]any) spec.AIResponse {
	t.Helper()
	srv := newOpenAICacheServer(t, usage)
	defer srv.Close()
	o := NewOpenAI(spec.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "gpt-test",
	}, srv.Client())
	resp, err := o.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	return resp
}

// TestOpenAIUsage_ParsesPromptCacheReads verifies the OpenAI shape
// (usage.prompt_tokens_details.cached_tokens) populates UsageDetails.CachedTokens.
func TestOpenAIUsage_ParsesPromptCacheReads(t *testing.T) {
	t.Parallel()
	resp := openAIUsageResponse(t, map[string]any{
		"total_tokens": 100,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": 42,
		},
	})
	if resp.TokenUsage != 100 {
		t.Errorf("TokenUsage = %d, want 100", resp.TokenUsage)
	}
	if resp.UsageDetails.CachedTokens != 42 {
		t.Errorf("CachedTokens = %d, want 42", resp.UsageDetails.CachedTokens)
	}
	if resp.UsageDetails.CacheWriteTokens != 0 {
		t.Errorf("CacheWriteTokens = %d, want 0 (OpenAI exposes no writes)", resp.UsageDetails.CacheWriteTokens)
	}
}

// TestOpenAIUsage_ParsesDeepSeekCacheFields verifies the DeepSeek
// OpenAI-compatible shape (prompt_cache_hit_tokens / prompt_cache_miss_tokens)
// maps reads to CachedTokens and misses (writes) to CacheWriteTokens.
func TestOpenAIUsage_ParsesDeepSeekCacheFields(t *testing.T) {
	t.Parallel()
	resp := openAIUsageResponse(t, map[string]any{
		"total_tokens": 50,
		"prompt_cache_hit_tokens": 30,
		"prompt_cache_miss_tokens": 20,
	})
	if resp.UsageDetails.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30 (prompt_cache_hit_tokens)", resp.UsageDetails.CachedTokens)
	}
	if resp.UsageDetails.CacheWriteTokens != 20 {
		t.Errorf("CacheWriteTokens = %d, want 20 (prompt_cache_miss_tokens)", resp.UsageDetails.CacheWriteTokens)
	}
}

// TestOpenAIUsage_ToleratesAbsentCacheFields verifies that providers which omit
// cache fields (the common case) leave UsageDetails at zero without error.
func TestOpenAIUsage_ToleratesAbsentCacheFields(t *testing.T) {
	t.Parallel()
	resp := openAIUsageResponse(t, map[string]any{
		"total_tokens": 7,
	})
	if resp.TokenUsage != 7 {
		t.Errorf("TokenUsage = %d, want 7", resp.TokenUsage)
	}
	if resp.UsageDetails.CachedTokens != 0 || resp.UsageDetails.CacheWriteTokens != 0 {
		t.Errorf("UsageDetails = %+v, want zero (no cache fields reported)", resp.UsageDetails)
	}
}

// TestOpenAIUsage_DeepSeekHitBeatsOpenAICachedWhenLarger verifies that when both
// OpenAI and DeepSeek shapes are present, the larger cache-read count wins.
func TestOpenAIUsage_DeepSeekHitBeatsOpenAICachedWhenLarger(t *testing.T) {
	t.Parallel()
	resp := openAIUsageResponse(t, map[string]any{
		"total_tokens": 100,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": 10,
		},
		"prompt_cache_hit_tokens": 25,
		"prompt_cache_miss_tokens": 5,
	})
	if resp.UsageDetails.CachedTokens != 25 {
		t.Errorf("CachedTokens = %d, want 25 (max of 10 and 25)", resp.UsageDetails.CachedTokens)
	}
	if resp.UsageDetails.CacheWriteTokens != 5 {
		t.Errorf("CacheWriteTokens = %d, want 5", resp.UsageDetails.CacheWriteTokens)
	}
}
