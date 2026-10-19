package api_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
)

const leakyDetail = "dial tcp 10.0.0.7:5432: secret-internal-detail"

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func leakyChatHandler() http.Handler {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.planErr = errors.New(leakyDetail)
	return api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})
}

// T-050: a 500 must not echo the error to the client; the error is logged
// together with the request ID that the response carries in X-Request-ID.
func TestInternalErrorIsGenericAndLoggedWithRequestID(t *testing.T) {
	logs := captureLogs(t)
	resp := request(leakyChatHandler(), http.MethodPost, "/v1/chat/completions", `{"messages":[{"role":"user","content":"Build me a REST API"}]}`)
	assertStatus(t, resp, http.StatusInternalServerError)
	if strings.Contains(resp.Body.String(), "secret-internal-detail") {
		t.Fatalf("response leaks internal error: %s", resp.Body.String())
	}
	id := resp.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("response has no X-Request-ID header")
	}
	var line string
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, "secret-internal-detail") && strings.Contains(l, id) && strings.Contains(l, "level=ERROR") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no ERROR log line with request id %q and the error; logs:\n%s", id, logs.String())
	}
}

func TestStreamingChatErrorFrameIsGeneric(t *testing.T) {
	logs := captureLogs(t)
	resp := request(leakyChatHandler(), http.MethodPost, "/v1/chat/completions", `{"stream":true,"messages":[{"role":"user","content":"Build me a REST API"}]}`)
	assertStatus(t, resp, http.StatusOK)
	if strings.Contains(resp.Body.String(), "secret-internal-detail") {
		t.Fatalf("stream frame leaks internal error: %s", resp.Body.String())
	}
	if !strings.Contains(logs.String(), "secret-internal-detail") {
		t.Fatalf("streaming planner error was not logged:\n%s", logs.String())
	}
}
