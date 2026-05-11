package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
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
	want := "custom prompt\n\n" + agenticToolUseSystemText()
	if got[0].Content != want {
		t.Fatalf("expected system message content %q, got %q", want, got[0].Content)
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

func TestBuildAgenticMessagesPreservesMemoryLessonsAndReplacesLegacy(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	lessons := "LESSONS LEARNED (from previous tasks):\n1. Symptom: x\n   Solution: y\n"
	legacy := legacyJSONCommandSystemSentinel + `, {"command":"..."}, or if the task is too complex...`
	messages := []gateway.PromptMessage{
		{Role: "system", Content: lessons},
		{Role: "system", Content: legacy},
		{Role: "user", Content: "task body"},
	}
	got := w.buildAgenticMessages(messages, models.AgentProfile{})
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0].Content != lessons {
		t.Fatalf("expected memory lessons preserved, got %q", got[0].Content)
	}
	if got[0].Role != "system" || got[1].Role != "system" || got[2].Role != "user" {
		t.Fatalf("unexpected roles: %+v", got)
	}
	if isLegacyJSONCommandSystemPrompt(got[1].Content) {
		t.Fatalf("expected legacy JSON prompt removed from agentic system, got %q", got[1].Content)
	}
	if !strings.Contains(got[1].Content, "You are an autonomous agent") || !strings.Contains(got[1].Content, "bash tool") {
		t.Fatalf("expected agentic tool instructions in second system, got %q", got[1].Content)
	}
	if got[1].Content != agenticToolUseSystemText() {
		t.Fatalf("expected bare agentic system when profile empty, got %q", got[1].Content)
	}
}

func TestExecuteAgenticTool_CapabilityRegistry(t *testing.T) {
	t.Parallel()
	registry := capabilities.NewRegistry()
	registry.Register("fake", fakeCapabilityCallAdapter{
		tools: []gateway.ToolDefinition{
			{Name: "capability_tool", Description: "x", Parameters: &gateway.FunctionParameters{Type: "object"}},
		},
	})
	w := &Worker{capabilities: registry}
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)
	toolToAdapter := map[string]string{"capability_tool": "fake"}
	out := w.executeAgenticTool(context.Background(), ex, gateway.ToolCall{
		Function: gateway.ToolCallFunction{Name: "capability_tool", Arguments: `{"id":"1"}`},
	}, toolToAdapter)
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v out=%s", err, out)
	}
	if payload["tool"] != "capability_tool" {
		t.Fatalf("expected tool name in payload, got %#v", payload)
	}
	args, _ := payload["args"].(map[string]any)
	if args["id"] != "1" {
		t.Fatalf("expected args id, got %#v", payload)
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

	tools, _ := w.agenticTools(context.Background(), executor)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools total (3 executor + 1 capability), got %d", len(tools))
	}
	if !containsTool(tools, "bash") {
		t.Fatal("expected bash tool from executor")
	}
	if !containsTool(tools, "capability_tool") {
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

type fakeCapabilityCallAdapter struct {
	tools []gateway.ToolDefinition
}

func (f fakeCapabilityCallAdapter) Name() string { return "fake" }

func (f fakeCapabilityCallAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityCallAdapter) CallTool(_ context.Context, name string, args map[string]any) (any, error) {
	return map[string]any{"tool": name, "args": args}, nil
}

func (f fakeCapabilityCallAdapter) Close() error { return nil }

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
