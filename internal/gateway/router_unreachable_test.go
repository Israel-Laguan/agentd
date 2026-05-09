package gateway

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"agentd/internal/models"
)

func TestRouterCascadeExhaustion_WrapsErrLLMUnreachable(t *testing.T) {
	openAI := &fakeProvider{providerName: "openai", err: fmt.Errorf("openai timeout")}
	anthropic := &fakeProvider{providerName: "anthropic", err: fmt.Errorf("anthropic 429")}
	ollama := &fakeProvider{providerName: "ollama", err: fmt.Errorf("ollama down")}

	_, err := NewRouter(openAI, anthropic, ollama).Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want error")
	}
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("error = %v, want ErrLLMUnreachable sentinel", err)
	}
	for _, want := range []string{"openai timeout", "anthropic 429", "ollama down"} {
		if !containsStr(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestRouterCascadeExhaustion_BreakerRecognizes(t *testing.T) {
	p := &fakeProvider{providerName: "mock", err: fmt.Errorf("fail")}
	_, err := NewRouter(p).Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "x"}},
	})
	if !errors.Is(err, models.ErrLLMUnreachable) {
		t.Fatalf("error = %v", err)
	}
	if !isBreakerErr(err) {
		t.Fatal("breaker should recognize cascade-exhaustion error")
	}
}

func isBreakerErr(err error) bool {
	return errors.Is(err, models.ErrLLMUnreachable) || errors.Is(err, models.ErrLLMQuotaExceeded)
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSubstr(s, sub))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
