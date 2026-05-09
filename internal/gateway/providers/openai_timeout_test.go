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
