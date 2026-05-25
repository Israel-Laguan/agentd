package providers

import (
	"bytes"
	"encoding/json"
	"log/slog"
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
