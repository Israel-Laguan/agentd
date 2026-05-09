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

func TestLlamaCpp_Generate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponseBody("hello from llamacpp", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL: srv.URL,
		Model:   "gpt-4",
	}, srv.Client())

	resp, err := l.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if resp.Content != "hello from llamacpp" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello from llamacpp")
	}
	if resp.ProviderUsed != "llamacpp" {
		t.Errorf("ProviderUsed = %q, want llamacpp", resp.ProviderUsed)
	}
}

func TestLlamaCpp_Timeout_CancelsSlowRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeOpenAIJSON(t, w, openAIResponseBody("late", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL: srv.URL,
		Model:   "gpt-4",
		Timeout: 20 * time.Millisecond,
	}, srv.Client())

	_, err := l.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("error = %v, want ErrLLMUnreachable", err)
	}
}

func TestLlamaCpp_Timeout_ZeroDoesNotEnforceTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOpenAIJSON(t, w, openAIResponseBody("fast", "gpt-4"))
	}))
	defer srv.Close()

	l := NewLlamaCpp(spec.ProviderConfig{
		BaseURL: srv.URL,
		Model:   "gpt-4",
		Timeout: 0,
	}, srv.Client())

	resp, err := l.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Content != "fast" {
		t.Fatalf("Content = %q", resp.Content)
	}
}