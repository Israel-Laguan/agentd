package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// TestBeat2CascadeToSecondary encodes docs/harness-reliability.md Beat 2 Scenario A:
// unreachable primary + healthy secondary → success via secondary.
func TestBeat2CascadeToSecondary(t *testing.T) {
	primary := &fakeProvider{providerName: "primary", err: fmt.Errorf("primary unreachable: connection refused")}
	secondary := &fakeProvider{
		providerName: "secondary",
		resp:         AIResponse{Content: "fallback ok", ProviderUsed: "secondary"},
	}
	resp, err := NewRouter(primary, secondary).Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "beat2 cascade"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v, want success via secondary", err)
	}
	if resp.ProviderUsed != "secondary" {
		t.Fatalf("ProviderUsed = %q, want secondary", resp.ProviderUsed)
	}
	if resp.Content != "fallback ok" {
		t.Fatalf("Content = %q, want %q", resp.Content, "fallback ok")
	}
	if primary.calls != 1 {
		t.Fatalf("primary calls = %d, want 1", primary.calls)
	}
	if secondary.calls != 1 {
		t.Fatalf("secondary calls = %d, want 1", secondary.calls)
	}
}

// TestBeat2ExhaustionBreakerClass encodes Beat 2 Scenario B:
// unreachable-only → ErrLLMUnreachable (breaker-class failure).
func TestBeat2ExhaustionBreakerClass(t *testing.T) {
	only := &fakeProvider{providerName: "primary", err: fmt.Errorf("dead upstream")}
	_, err := NewRouter(only).Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "beat2 breaker"}},
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want ErrLLMUnreachable")
	}
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("error = %v, want ErrLLMUnreachable", err)
	}
	if !isBreakerErr(err) {
		t.Fatal("breaker should classify cascade exhaustion as breaker failure")
	}
}

// TestRouterCascadesToSecondaryWhenPrimaryFails encodes T-025 Part A with fake
// providers: a failing primary cascades to the secondary, and ProviderUsed and
// the content come from the secondary. It does not exercise the real openai
// adapter built by NewRouterFromConfigs.
func TestRouterCascadesToSecondaryWhenPrimaryFails(t *testing.T) {
	// Secondary is always reachable and returns a response
	secondary := &fakeProvider{
		providerName: "secondary",
		resp:         AIResponse{Content: "cascade success", ProviderUsed: "secondary"},
	}

	router := NewRouter(&fakeProvider{providerName: "primary", err: fmt.Errorf("connection refused")}, secondary)
	resp, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test cascade"}},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v, want nil", err)
	}
	if resp.ProviderUsed != "secondary" {
		t.Fatalf("ProviderUsed = %q, want secondary", resp.ProviderUsed)
	}
	if resp.Content != "cascade success" {
		t.Fatalf("Content = %q, want cascade success", resp.Content)
	}
}

// TestNewRouterFromConfigsCascadesToLiveSecondary is the real-adapter half of
// T-025 Part A: NewRouterFromConfigs with a dead primary (http://127.0.0.1:1)
// and an httptest.Server secondary. The cascade must reach the secondary over
// HTTP and ProviderUsed must be "secondary".
func TestNewRouterFromConfigsCascadesToLiveSecondary(t *testing.T) {
	var secondaryHits atomic.Int64
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondaryHits.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-beat2",
			"object": "chat.completion",
			"created": 1760000000,
			"model": "mock-secondary",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "fallback ok"}, "finish_reason": "stop"}],
			"usage": {"total_tokens": 3}
		}`))
	}))
	defer secondary.Close()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{
		{Name: "primary", Adapter: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "primary-model"},
		{Name: "secondary", Adapter: "openai", BaseURL: secondary.URL + "/v1", Model: "secondary-model"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs() error = %v", err)
	}

	resp, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "beat2 cascade"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v, want success via secondary", err)
	}
	if resp.ProviderUsed != "secondary" {
		t.Fatalf("ProviderUsed = %q, want secondary", resp.ProviderUsed)
	}
	if resp.Content != "fallback ok" {
		t.Fatalf("Content = %q, want %q", resp.Content, "fallback ok")
	}
	if secondaryHits.Load() == 0 {
		t.Fatal("secondary server received no requests")
	}
}
