package gateway

import (
	"context"
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
