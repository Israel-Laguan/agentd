package gateway

import (
	"context"
	"fmt"
	"testing"
)

func TestRoleRoutingDispatchesToMappedProvider(t *testing.T) {
	chat := &fakeProvider{providerName: "synth-chat", resp: AIResponse{Content: "smart", ProviderUsed: "synth-chat"}}
	worker := &fakeProvider{providerName: "synth-worker", resp: AIResponse{Content: "code", ProviderUsed: "synth-worker"}}
	memory := &fakeProvider{providerName: "synth-memory", resp: AIResponse{Content: "cheap", ProviderUsed: "synth-memory"}}

	routes := map[Role]RoleTarget{
		RoleChat:   {Provider: "synth-chat", Model: "gpt-4o"},
		RoleWorker: {Provider: "synth-worker", Model: "claude-3-haiku"},
		RoleMemory: {Provider: "synth-memory", Model: "llama3-8b"},
	}
	router := NewRouter(chat, worker, memory).WithRoleRouting(routes)

	tests := []struct {
		role         Role
		wantProvider string
	}{
		{RoleChat, "synth-chat"},
		{RoleWorker, "synth-worker"},
		{RoleMemory, "synth-memory"},
	}
	for _, tt := range tests {
		chat.calls, worker.calls, memory.calls = 0, 0, 0
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
	chat := &fakeProvider{providerName: "synth-chat", resp: AIResponse{Content: "ok", ProviderUsed: "synth-chat"}}
	memory := &fakeProvider{providerName: "synth-memory", resp: AIResponse{Content: "local", ProviderUsed: "synth-memory"}}

	routes := map[Role]RoleTarget{
		RoleChat: {Provider: "synth-memory"},
	}
	router := NewRouter(chat, memory).WithRoleRouting(routes)

	resp, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		Role:     RoleChat,
		Provider: "synth-chat",
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.ProviderUsed != "synth-chat" {
		t.Fatalf("explicit Provider should override role routing, got %q", resp.ProviderUsed)
	}
}

func TestRoleRoutingCascadesToNextOnFailure(t *testing.T) {
	workerFail := &fakeProvider{providerName: "synth-worker-fail", err: fmt.Errorf("worker quota exhausted")}
	horde := &fakeProvider{providerName: "synth-horde", resp: AIResponse{Content: "ok", ProviderUsed: "synth-horde"}}

	routes := map[Role]RoleTarget{
		RoleWorker: {Provider: "synth-worker-fail", Model: "worker-model"},
	}
	router := NewRouter(workerFail, horde).WithRoleRouting(routes)

	resp, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		Role:     RoleWorker,
		// Provider is intentionally empty: should be filled by role routing, then cascade on failure
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "synth-horde" {
		t.Errorf("ProviderUsed = %q, want synth-horde", resp.ProviderUsed)
	}
	if workerFail.calls != 1 {
		t.Errorf("synth-worker-fail.calls = %d, want 1 (must be attempted first)", workerFail.calls)
	}
	if horde.calls != 1 {
		t.Errorf("synth-horde.calls = %d, want 1", horde.calls)
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
