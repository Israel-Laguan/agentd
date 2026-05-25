package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestOpenAIGenerate_CustomProviderNameInResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOpenAIJSON(t, w, openAIResponseBody("ok", "poolside-model"))
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{
		Name:    "poolside",
		Adapter: "openai",
		BaseURL: srv.URL + "/v1",
		APIKey:  "sk-test",
		Model:   "poolside-model",
	}, srv.Client())

	resp, err := o.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "poolside" {
		t.Fatalf("ProviderUsed = %q, want poolside", resp.ProviderUsed)
	}
}

func TestOpenAIGenerate_GeminiLegacyAdapterAliasProviderUsed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOpenAIJSON(t, w, openAIResponseBody("ok", "gemini-2.5-flash"))
	}))
	defer srv.Close()

	got, err := AppendFromConfig(nil, spec.ProviderConfig{
		Adapter: "gemini",
		BaseURL: srv.URL + "/v1",
		APIKey:  "gem-key",
		Model:   "gemini-2.5-flash",
	})
	if err != nil {
		t.Fatalf("AppendFromConfig() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	openAI, ok := got[0].(*OpenAI)
	if !ok {
		t.Fatalf("backend type = %T, want *OpenAI", got[0])
	}
	openAI.client = srv.Client()

	resp, err := openAI.Generate(context.Background(), spec.AIRequest{
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "gemini" {
		t.Fatalf("ProviderUsed = %q, want gemini", resp.ProviderUsed)
	}
}
