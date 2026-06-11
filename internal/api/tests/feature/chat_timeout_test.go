package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/models"
)

const expectedSystemTimeoutMessage = "[SYSTEM] Communication with AI core timed out. Please try your request again."

func TestLLMUnreachableReturnsSystemTimeoutReply(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.planErr = models.ErrLLMUnreachable
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	resp := request(handler, http.MethodPost, "/v1/chat/completions", `{"messages":[{"role":"user","content":"Build me a REST API"}]}`)
	assertStatus(t, resp, http.StatusOK)
	assertJSONField(t, resp, "object", "chat.completion")
	assertChatContent(t, resp, expectedSystemTimeoutMessage)
}

func TestDeadlineExceededReturnsSystemTimeoutReply(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.intentErr = context.DeadlineExceeded
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	resp := request(handler, http.MethodPost, "/v1/chat/completions", `{"messages":[{"role":"user","content":"Build me a REST API"}]}`)
	assertStatus(t, resp, http.StatusOK)
	assertJSONField(t, resp, "object", "chat.completion")
	assertChatContent(t, resp, expectedSystemTimeoutMessage)
}

func TestNonTimeoutGatewayErrorStillReturns500(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.planErr = errors.New("some other error")
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	resp := request(handler, http.MethodPost, "/v1/chat/completions", `{"messages":[{"role":"user","content":"Build me a REST API"}]}`)
	assertStatus(t, resp, http.StatusInternalServerError)
}

func assertChatContent(t *testing.T, resp *httptest.ResponseRecorder, want string) {
	t.Helper()
	content := decodeBody(t, resp)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}
