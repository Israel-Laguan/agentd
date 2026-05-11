package worker

import (
	"context"
	"database/sql"
	"testing"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestBuildAgenticMessagesReplacesLeadingSystemMessage(t *testing.T) {
	w := &Worker{}
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "existing prompt"},
		{Role: "user", Content: "do work"},
	}
	profile := models.AgentProfile{
		SystemPrompt: sql.NullString{String: "custom prompt", Valid: true},
	}

	got := w.buildAgenticMessages(messages, profile)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "system" {
		t.Fatalf("expected first role system, got %q", got[0].Role)
	}
	if got[1].Role != "user" {
		t.Fatalf("expected second role user, got %q", got[1].Role)
	}
	if got[0].Content == "existing prompt" {
		t.Fatal("expected system message content to be replaced")
	}
	if countRole(got, "system") != 1 {
		t.Fatalf("expected one system message, got %d", countRole(got, "system"))
	}
}

func TestBuildAgenticMessagesInsertsBeforeFirstUserWhenNoLeadingSystem(t *testing.T) {
	w := &Worker{}
	messages := []gateway.PromptMessage{
		{Role: "assistant", Content: "prior response"},
		{Role: "user", Content: "run task"},
	}

	got := w.buildAgenticMessages(messages, models.AgentProfile{})
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[1].Role != "system" {
		t.Fatalf("expected inserted message at index 1 to be system, got %q", got[1].Role)
	}
	if got[2].Role != "user" {
		t.Fatalf("expected user message after inserted system, got %q", got[2].Role)
	}
}

func TestAgenticToolsIncludesExecutorAndCapabilityTools(t *testing.T) {
	registry := capabilities.NewRegistry()
	registry.Register("fake", fakeCapabilityAdapter{
		tools: []gateway.ToolDefinition{
			{
				Name:        "capability_tool",
				Description: "tool from capability adapter",
				Parameters:  &gateway.FunctionParameters{Type: "object"},
			},
		},
	})

	w := &Worker{capabilities: registry}
	executor := NewToolExecutor(nil, "", nil, 0)

	got := w.agenticTools(context.Background(), executor)
	if len(got) != 4 {
		t.Fatalf("expected 4 tools total (3 executor + 1 capability), got %d", len(got))
	}
	if !containsTool(got, "bash") {
		t.Fatal("expected bash tool from executor")
	}
	if !containsTool(got, "capability_tool") {
		t.Fatal("expected capability tool")
	}
}

type fakeCapabilityAdapter struct {
	tools []gateway.ToolDefinition
}

func (f fakeCapabilityAdapter) Name() string { return "fake" }

func (f fakeCapabilityAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityAdapter) CallTool(context.Context, string, map[string]any) (any, error) {
	return nil, nil
}

func (f fakeCapabilityAdapter) Close() error { return nil }

func containsTool(tools []gateway.ToolDefinition, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func countRole(messages []gateway.PromptMessage, role string) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == role {
			count++
		}
	}
	return count
}
