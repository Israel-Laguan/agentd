package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"agentd/internal/models"
)

func (s *gatewayScenario) openAIWithTimeout(_ context.Context, ms int) error {
	s.providerContent = "openai"
	s.budget = ms
	return nil
}

func (s *gatewayScenario) ollamaWithTimeout(_ context.Context, ms int) error {
	s.providerContent = "ollama"
	s.budget = ms
	return nil
}

func (s *gatewayScenario) mockServerDelays(_ context.Context, ms int) error {
	delay := time.Duration(ms) * time.Millisecond
	timeout := time.Duration(s.budget) * time.Millisecond

	switch s.providerContent {
	case "openai":
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(delay)
			writeJSONHelper(w, openAIResponseBody("late", "gpt-test"))
		}))
		o := NewOpenAI(ProviderConfig{BaseURL: srv.URL + "/v1", Model: "gpt-test", Timeout: timeout}, srv.Client())
		s.providerResp, s.providerErr = o.Generate(context.Background(), AIRequest{
			Messages: []PromptMessage{{Role: "user", Content: "hello"}},
		})
	case "ollama":
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(delay)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{"role": "assistant", "content": "late"},
				"model":   "llama3",
			})
		}))
		o := NewOllama(ProviderConfig{BaseURL: srv.URL, Model: "llama3", Timeout: timeout}, srv.Client())
		s.providerResp, s.providerErr = o.Generate(context.Background(), AIRequest{
			Messages: []PromptMessage{{Role: "user", Content: "hello"}},
		})
	}
	return nil
}

func (s *gatewayScenario) mockServerImmediate(context.Context) error {
	timeout := time.Duration(s.budget) * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONHelper(w, openAIResponseBody("fast", "gpt-test"))
	}))
	o := NewOpenAI(ProviderConfig{BaseURL: srv.URL + "/v1", Model: "gpt-test", Timeout: timeout}, srv.Client())
	s.providerResp, s.providerErr = o.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "hello"}},
	})
	return nil
}

func (s *gatewayScenario) sendToOpenAI(context.Context) error {
	return nil
}

func (s *gatewayScenario) sendToOllama(context.Context) error {
	return nil
}

func (s *gatewayScenario) requestFailsUnreachable(context.Context) error {
	if s.providerErr == nil {
		return fmt.Errorf("expected error, got nil")
	}
	if !errors.Is(s.providerErr, models.ErrLLMUnreachable) {
		return fmt.Errorf("error = %v, want ErrLLMUnreachable", s.providerErr)
	}
	return nil
}

func (s *gatewayScenario) requestSucceedsWithContent(_ context.Context, content string) error {
	if s.providerErr != nil {
		return fmt.Errorf("error = %v", s.providerErr)
	}
	if s.providerResp.Content != content {
		return fmt.Errorf("Content = %q, want %q", s.providerResp.Content, content)
	}
	return nil
}

func writeJSONHelper(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
