package queue

import (
	"context"
	"database/sql"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// TestWorkerForwardsAgentProfileOverrides ensures that profile.Provider /
// Model / MaxTokens are surfaced on the AIRequest so the gateway router
// can honor per-agent LLM choices instead of falling back to role routing
// or the first configured provider.
func TestWorkerForwardsAgentProfileOverrides(t *testing.T) {
	store := newWorkerStore()
	store.profile = models.AgentProfile{
		ID: "default", Provider: "anthropic", Model: "claude-3-haiku",
		Temperature: 0.4, MaxTokens: 4321,
		SystemPrompt: sql.NullString{String: "You are precise.", Valid: true},
	}
	gw := &fakeGateway{content: `{"command":"echo hi"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "hi"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if len(gw.requests) == 0 {
		t.Fatal("no AIRequest captured")
	}
	req := gw.requests[0]
	if req.Provider != "anthropic" {
		t.Fatalf("Provider = %q, want anthropic", req.Provider)
	}
	if req.Model != "claude-3-haiku" {
		t.Fatalf("Model = %q, want claude-3-haiku", req.Model)
	}
	if req.MaxTokens != 4321 {
		t.Fatalf("MaxTokens = %d, want 4321", req.MaxTokens)
	}
	if req.Temperature != 0.4 {
		t.Fatalf("Temperature = %v, want 0.4", req.Temperature)
	}
}
