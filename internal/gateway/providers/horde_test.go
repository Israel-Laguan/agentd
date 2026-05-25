package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func sampleAIRequest() spec.AIRequest {
	return spec.AIRequest{Messages: []spec.PromptMessage{{Role: "user", Content: "say hello"}}, JSONMode: true}
}

func TestHordeGenerateSubmitsAndPollsUntilDone(t *testing.T) {
	var submitted hordeSubmitRequest
	srv := newHordeSuccessServer(t, &submitted)
	defer srv.Close()

	provider := NewHorde(spec.ProviderConfig{
		BaseURL: srv.URL,
		APIKey:  "key",
		Model:   "horde-model",
		Timeout: time.Second,
		Options: map[string]any{"poll_interval": time.Millisecond},
	}, srv.Client())
	resp, err := provider.Generate(context.Background(), spec.AIRequest{
		Messages:    []spec.PromptMessage{{Role: "system", Content: "Return JSON."}, {Role: "user", Content: "Do work."}},
		Temperature: 0.2,
		MaxTokens:   123,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	assertHordeResponse(t, resp)
	assertHordeSubmission(t, submitted)
}

func newHordeSuccessServer(t *testing.T, submitted *hordeSubmitRequest) *httptest.Server {
	t.Helper()
	statusCalls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "key" {
			t.Fatalf("apikey header = %q", r.Header.Get("apikey"))
		}
		if r.Header.Get("Client-Agent") == "" {
			t.Fatalf("missing Client-Agent header")
		}
		switch r.URL.Path {
		case "/v2/generate/text/async":
			if err := json.NewDecoder(r.Body).Decode(submitted); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			writeJSON(t, w, hordeAsyncResponse{ID: "request-id"})
		case "/v2/generate/text/status/request-id":
			statusCalls++
			if statusCalls == 1 {
				writeJSON(t, w, hordeStatusResponse{IsPossible: true})
				return
			}
			writeJSON(t, w, hordeStatusResponse{
				Done:       true,
				IsPossible: true,
				Generations: []hordeGeneration{{
					Text:  `{"command":"echo horde"}`,
					Model: "horde-model",
				}},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
}

func assertHordeResponse(t *testing.T, resp spec.AIResponse) {
	t.Helper()
	if resp.Content != `{"command":"echo horde"}` || resp.ProviderUsed != "horde" || resp.ModelUsed != "horde-model" {
		t.Fatalf("response = %#v", resp)
	}
}

func assertHordeSubmission(t *testing.T, submitted hordeSubmitRequest) {
	t.Helper()
	if submitted.Params.MaxLength != 123 || submitted.Params.Temperature != 0.2 || submitted.Params.N != 1 {
		t.Fatalf("submitted params = %#v", submitted.Params)
	}
	if len(submitted.Models) != 1 || submitted.Models[0] != "horde-model" {
		t.Fatalf("models = %#v", submitted.Models)
	}
	if !strings.Contains(submitted.Prompt, "System:\nReturn JSON.") || !strings.Contains(submitted.Prompt, "User:\nDo work.") {
		t.Fatalf("prompt = %q", submitted.Prompt)
	}
}

func TestHordeGenerateTimesOutWhilePolling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/generate/text/async":
			writeJSON(t, w, hordeAsyncResponse{ID: "request-id"})
		case "/v2/generate/text/status/request-id":
			writeJSON(t, w, hordeStatusResponse{IsPossible: true})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, err := NewHorde(spec.ProviderConfig{
		BaseURL: srv.URL,
		Timeout: 5 * time.Millisecond,
		Options: map[string]any{"poll_interval": time.Millisecond},
	}, srv.Client()).Generate(context.Background(), sampleAIRequest())
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("Generate() error = %v, want ErrLLMUnreachable", err)
	}
}

func TestHordeGenerateFailsFaultedJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/generate/text/async":
			writeJSON(t, w, hordeAsyncResponse{ID: "request-id"})
		case "/v2/generate/text/status/request-id":
			writeJSON(t, w, hordeStatusResponse{Faulted: true, IsPossible: true})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, err := NewHorde(spec.ProviderConfig{BaseURL: srv.URL, Timeout: time.Second}, srv.Client()).
		Generate(context.Background(), sampleAIRequest())
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("Generate() error = %v, want ErrLLMUnreachable", err)
	}
}

func TestHordeMapsQuotaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "too many prompts", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := NewHorde(spec.ProviderConfig{BaseURL: srv.URL, Timeout: time.Second}, srv.Client()).
		Generate(context.Background(), sampleAIRequest())
	if !errors.Is(err, models.ErrLLMQuotaExceeded) {
		t.Fatalf("Generate() error = %v, want ErrLLMQuotaExceeded", err)
	}
}

func TestHordePollIntervalFromOptionsString(t *testing.T) {
	// poll_interval arriving as a YAML-decoded string (e.g. "5s") must be
	// parsed and applied correctly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/generate/text/async":
			writeJSON(t, w, hordeAsyncResponse{ID: "req-str"})
		case "/v2/generate/text/status/req-str":
			writeJSON(t, w, hordeStatusResponse{
				Done: true, IsPossible: true,
				Generations: []hordeGeneration{{Text: "ok", Model: "m"}},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	h := NewHorde(spec.ProviderConfig{
		BaseURL: srv.URL,
		Timeout: time.Second,
		Options: map[string]any{"poll_interval": "5ms"}, // string form as from YAML
	}, srv.Client())
	if h.pollInterval != 5*time.Millisecond {
		t.Fatalf("pollInterval = %v, want 5ms", h.pollInterval)
	}
	if _, err := h.Generate(context.Background(), sampleAIRequest()); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestHordeUnknownOptionLogsWarning(t *testing.T) {
	// An unrecognized option key must produce a slog.Warn but not fail.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	old := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })

	h := NewHorde(spec.ProviderConfig{
		Options: map[string]any{"unsupported_key": "value"},
	}, nil)
	if h == nil {
		t.Fatal("NewHorde returned nil")
	}
	if !strings.Contains(buf.String(), "unknown provider option ignored") {
		t.Errorf("expected warning for unknown option; log = %q", buf.String())
	}
	if !strings.Contains(buf.String(), "unsupported_key") {
		t.Errorf("expected warning to name the unknown key; log = %q", buf.String())
	}
}

func TestMessagesToHordePrompt(t *testing.T) {
	prompt := messagesToHordePrompt([]spec.PromptMessage{
		{Role: "system", Content: "Rules"},
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
	})
	for _, want := range []string{"System:\nRules", "User:\nQuestion", "Assistant:\nAnswer"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt %q missing %q", prompt, want)
		}
	}
	if !strings.HasSuffix(prompt, "Assistant:") {
		t.Fatalf("prompt = %q, want Assistant suffix", prompt)
	}
}
