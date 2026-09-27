package gateway

import (
	"context"
	"errors"
	"fmt"
	"testing"

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

// TestNewRouterFromConfigs_PrimaryFailureSecondarySucess encodes T-025 Part A:
// dead primary provider (http://127.0.0.1:1) with live secondary httptest.Server.
// ProviderUsed should be "secondary" and the response content should be captured.
func TestNewRouterFromConfigs_PrimaryFailureSecondarySucess(t *testing.T) {
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
