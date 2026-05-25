package gateway

import (
	"context"
	"fmt"
	"testing"
)

func TestRoleRoutingDispatchesToMappedProvider(t *testing.T) {
	openAI := &fakeProvider{providerName: "openai", resp: AIResponse{Content: "smart", ProviderUsed: "openai"}}
	anthropic := &fakeProvider{providerName: "anthropic", resp: AIResponse{Content: "code", ProviderUsed: "anthropic"}}
	ollama := &fakeProvider{providerName: "ollama", resp: AIResponse{Content: "cheap", ProviderUsed: "ollama"}}

	routes := map[Role]RoleTarget{
		RoleChat:   {Provider: "openai", Model: "gpt-4o"},
		RoleWorker: {Provider: "anthropic", Model: "claude-3-haiku"},
		RoleMemory: {Provider: "ollama", Model: "llama3-8b"},
	}
	router := NewRouter(openAI, anthropic, ollama).WithRoleRouting(routes)

	tests := []struct {
		role         Role
		wantProvider string
	}{
		{RoleChat, "openai"},
		{RoleWorker, "anthropic"},
		{RoleMemory, "ollama"},
	}
	for _, tt := range tests {
		openAI.calls, anthropic.calls, ollama.calls = 0, 0, 0
		resp, err := router.Generate(context.Background(), AIRequest{
			Messages: []PromptMessage{{Role: "user", Content: "test"}},
			Role:     tt.role,
		})
		if err != nil {
			t.Fatalf("role=%s error = %v", tt.role, err)
		}
		if resp.ProviderUsed != tt.wantProvider {
			t.Fatalf("role=%s ProviderUsed = %q, want %q", tt.role, resp.ProviderUsed, tt.wantProvider)
		}
	}
}

func TestRoleRoutingExplicitProviderOverridesRole(t *testing.T) {
	openAI := &fakeProvider{providerName: "openai", resp: AIResponse{Content: "ok", ProviderUsed: "openai"}}
	ollama := &fakeProvider{providerName: "ollama", resp: AIResponse{Content: "local", ProviderUsed: "ollama"}}

	routes := map[Role]RoleTarget{
		RoleChat: {Provider: "ollama"},
	}
	router := NewRouter(openAI, ollama).WithRoleRouting(routes)

	resp, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		Role:     RoleChat,
		Provider: "openai",
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.ProviderUsed != "openai" {
		t.Fatalf("explicit Provider should override role routing, got %q", resp.ProviderUsed)
	}
}

func TestRoleRoutingCascadesToNextOnFailure(t *testing.T) {
	gemini := &fakeProvider{providerName: "gemini", err: fmt.Errorf("gemini quota exhausted")}
	horde := &fakeProvider{providerName: "horde", resp: AIResponse{Content: "ok", ProviderUsed: "horde"}}

	routes := map[Role]RoleTarget{
		RoleWorker: {Provider: "gemini", Model: "gemini-2.5-flash"},
	}
	router := NewRouter(gemini, horde).WithRoleRouting(routes)

	resp, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		Role:     RoleWorker,
		// Provider is intentionally empty: should be filled by role routing, then cascade on failure
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "horde" {
		t.Errorf("ProviderUsed = %q, want horde", resp.ProviderUsed)
	}
	if gemini.calls != 1 {
		t.Errorf("gemini.calls = %d, want 1 (must be attempted first)", gemini.calls)
	}
	if horde.calls != 1 {
		t.Errorf("horde.calls = %d, want 1", horde.calls)
	}
}

func TestRoleRoutingNoRoutesFallsThrough(t *testing.T) {
	p := &fakeProvider{providerName: "mock", resp: AIResponse{Content: "ok", ProviderUsed: "mock"}}
	router := NewRouter(p)

	resp, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		Role:     RoleWorker,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.ProviderUsed != "mock" {
		t.Fatalf("ProviderUsed = %q", resp.ProviderUsed)
	}
}
