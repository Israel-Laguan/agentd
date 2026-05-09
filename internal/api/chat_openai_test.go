package api_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"

	openai "github.com/openai/openai-go/v3"
)

// TestChatCompletionRoundTripsThroughOpenAITypes verifies the response
// body emitted by our /v1/chat/completions endpoint can be parsed by the
// official openai/openai-go SDK without losing the assistant content. This
// is the live wire-spec witness referenced from the chat handler doc.
func TestChatCompletionRoundTripsThroughOpenAITypes(t *testing.T) {
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store: store, Gateway: newAPIGateway(), Bus: bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	body := `{"model":"agentd","messages":[{"role":"user","content":"A Python script to scrape a website"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	var parsed openai.ChatCompletion
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("openai.ChatCompletion unmarshal: %v\nbody=%s", err, resp.Body.String())
	}
	if parsed.Object != "chat.completion" {
		t.Fatalf("object = %q, want chat.completion", parsed.Object)
	}
	if len(parsed.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(parsed.Choices))
	}
	if parsed.Choices[0].Message.Content == "" {
		t.Fatal("openai-parsed content is empty")
	}
}

// TestChatCompletionEmitsToolCallsWhenRequested ensures that a client
// opting into OpenAI tool calling (by sending a non-empty tools array)
// gets the plan back as a structured create_plan tool call instead of
// (or in addition to) plain content.
func TestChatCompletionEmitsToolCallsWhenRequested(t *testing.T) {
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store: store, Gateway: newAPIGateway(), Bus: bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	body := `{
		"model":"agentd",
		"messages":[{"role":"user","content":"A Python script to scrape a website"}],
		"tools":[{"type":"function","function":{"name":"create_plan"}}]
	}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	var parsed openai.ChatCompletion
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("openai.ChatCompletion unmarshal: %v", err)
	}
	choice := parsed.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(choice.Message.ToolCalls))
	}
	call := choice.Message.ToolCalls[0]
	if call.Function.Name != "create_plan" {
		t.Fatalf("tool call name = %q, want create_plan", call.Function.Name)
	}
	if call.Function.Arguments == "" {
		t.Fatal("tool call arguments empty; expected DraftPlan JSON")
	}
}

// TestChatCompletionStatusReportToolCall verifies the status_check intent
// emits a status_report tool call when tools are requested.
func TestChatCompletionStatusReportToolCall(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.intent = &gateway.IntentAnalysis{Intent: "status_check", Reason: "user asked for status"}
	handler := api.NewHandler(api.ServerDeps{
		Store: store, Gateway: gw, Bus: bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	body := `{
		"model":"agentd",
		"messages":[{"role":"user","content":"What's going on?"}],
		"tools":[{"type":"function","function":{"name":"status_report"}}]
	}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	var parsed openai.ChatCompletion
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("openai.ChatCompletion unmarshal: %v", err)
	}
	if len(parsed.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(parsed.Choices[0].Message.ToolCalls))
	}
	if name := parsed.Choices[0].Message.ToolCalls[0].Function.Name; name != "status_report" {
		t.Fatalf("tool call name = %q, want status_report", name)
	}
}

// TestChatCompletionStreamsChunks verifies that stream:true switches the
// response to text/event-stream and emits at least one chat.completion.chunk
// frame followed by [DONE].
func TestChatCompletionStreamsChunks(t *testing.T) {
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store: store, Gateway: newAPIGateway(), Bus: bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	body := `{"model":"agentd","stream":true,"messages":[{"role":"user","content":"A Python script to scrape a website"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	scanner := bufio.NewScanner(rec.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var sawChunk, sawDone bool
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			t.Fatalf("unexpected SSE line: %q", line)
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			sawDone = true
			continue
		}
		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("ChatCompletionChunk unmarshal: %v\npayload=%s", err, payload)
		}
		if chunk.Object != "chat.completion.chunk" {
			t.Fatalf("chunk.object = %q, want chat.completion.chunk", chunk.Object)
		}
		sawChunk = true
	}
	if !sawChunk {
		t.Fatal("no streaming chunk frames observed")
	}
	if !sawDone {
		t.Fatal("missing [DONE] terminator")
	}
}
