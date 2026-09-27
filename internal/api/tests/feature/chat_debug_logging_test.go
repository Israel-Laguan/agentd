package api_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
)

// newRealChatHandler wires the actual production request pipeline used by
// T-022/US-006 (Frontdesk Planner -> Router -> OpenAI-compatible provider)
// against a fake upstream HTTP server, instead of the api_test package's
// usual stub gateway. This lets debug-logging tests observe genuine
// cross-boundary log output (chat intake, router cascade, provider adapter)
// for a single traced request.
func newRealChatHandler(t *testing.T, upstream *httptest.Server) http.Handler {
	t.Helper()
	provider := gateway.NewOpenAI(gateway.ProviderConfig{
		BaseURL: upstream.URL + "/v1",
		Model:   "gpt-test",
		APIKey:  "sk-test-secret-should-never-be-logged",
		Timeout: 5 * time.Second,
	}, upstream.Client())
	router := gateway.NewRouter(provider)

	store := newAPITestStore()
	return api.NewHandler(api.ServerDeps{
		Store: store, Gateway: router, Bus: bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
}

// captureDebugLogs redirects the global slog default logger to a buffer at
// the given level for the duration of fn, then restores it.
func captureDebugLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// draftPlanUpstream returns a fake OpenAI-compatible server that always
// answers with a minimal valid DraftPlan JSON payload, echoing a canary
// string so the test can confirm prompt content did NOT leak into logs
// while still exercising a realistic round trip.
func draftPlanUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := `{"project_name":"demo","tasks":[{"title":"Do the thing","description":"desc"}]}`
		payload, _ := json.Marshal(content)
		_, _ = w.Write([]byte(`{
			"model":"gpt-test",
			"choices":[{"message":{"role":"assistant","content":` + string(payload) + `}}],
			"usage":{"total_tokens":11}
		}`))
	}))
}

// TestChatPipeline_CorrelationIDTracesIntakeRouterProvider verifies that a
// single correlation ID assigned at chat intake appears, identically, in
// debug log lines emitted by the router and the provider adapter for the
// same request (T-022 AC: "one identifier"; US-006 AC: HTTP intake through
// the final gateway/provider operation).
func TestChatPipeline_CorrelationIDTracesIntakeRouterProvider(t *testing.T) {
	upstream := draftPlanUpstream(t)
	defer upstream.Close()
	handler := newRealChatHandler(t, upstream)

	buf := captureDebugLogs(t, slog.LevelDebug)

	body := `{"model":"agentd","approved_scopes":["proj-1"],"messages":[{"role":"user","content":"plan a python scraper"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	logs := buf.String()

	if !strings.Contains(logs, "chat intake: request received") {
		t.Fatalf("missing chat intake lifecycle log; logs=%s", logs)
	}
	if !strings.Contains(logs, "router cascade starting") {
		t.Fatalf("missing router lifecycle log; logs=%s", logs)
	}
	if !strings.Contains(logs, "openai: sending request") {
		t.Fatalf("missing provider lifecycle log; logs=%s", logs)
	}

	ids := regexp.MustCompile(`correlation_id=(\S+)`).FindAllStringSubmatch(logs, -1)
	if len(ids) < 3 {
		t.Fatalf("expected correlation_id on at least 3 log lines (intake, router, provider); got %d; logs=%s", len(ids), logs)
	}
	first := ids[0][1]
	if first == "" {
		t.Fatal("correlation_id was empty")
	}
	for _, m := range ids {
		if m[1] != first {
			t.Fatalf("correlation_id mismatch across boundaries: %q vs %q; logs=%s", m[1], first, logs)
		}
	}
}

// TestChatPipeline_DebugLogsSuppressedAtDefaultLevel confirms the pipeline
// stays silent at the default (info) log level, per T-022's "debug logging
// is silent by default" acceptance criterion.
func TestChatPipeline_DebugLogsSuppressedAtDefaultLevel(t *testing.T) {
	upstream := draftPlanUpstream(t)
	defer upstream.Close()
	handler := newRealChatHandler(t, upstream)

	buf := captureDebugLogs(t, slog.LevelInfo)

	body := `{"model":"agentd","approved_scopes":["proj-1"],"messages":[{"role":"user","content":"plan a python scraper"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	logs := buf.String()
	for _, marker := range []string{
		"chat intake: request received",
		"chat intake: parsed request",
		"router cascade starting",
		"router trying provider",
		"openai: sending request",
		"openai: response received",
	} {
		if strings.Contains(logs, marker) {
			t.Fatalf("debug marker %q leaked at info level; logs=%s", marker, logs)
		}
	}
}

// TestChatPipeline_NoSecretsOrPromptContentInLogs verifies that even at
// debug level, no API key and no raw prompt/user content is ever emitted
// across the intake -> router -> provider chain (T-022/US-006 hard
// constraint).
func TestChatPipeline_NoSecretsOrPromptContentInLogs(t *testing.T) {
	upstream := draftPlanUpstream(t)
	defer upstream.Close()
	handler := newRealChatHandler(t, upstream)

	buf := captureDebugLogs(t, slog.LevelDebug)

	secretMarker := "sk-test-secret-should-never-be-logged"
	promptMarker := "confidential-canary-prompt-content-xyz"
	body := `{"model":"agentd","approved_scopes":["proj-1"],"messages":[{"role":"user","content":"` + promptMarker + `"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	logs := buf.String()
	for _, forbidden := range []string{secretMarker, "Bearer " + secretMarker, promptMarker} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("logs leaked %q; logs=%s", forbidden, logs)
		}
	}
}
