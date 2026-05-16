package worker

import (
	"context"
	"encoding/json"
	"testing"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
)

func TestSubagentDelegate_CapabilityToolsFilteredCaseInsensitive(t *testing.T) {
	t.Parallel()

	registry := capabilities.NewRegistry()
	registry.Register("fake", fakeCapabilityAdapter{
		tools: []gateway.ToolDefinition{
			{Name: "capability_read", Description: "read capability"},
			{Name: "capability_write", Description: "write capability"},
		},
	})

	def := SubagentDefinition{
		Name:           "cap-agent",
		Purpose:        "use capabilities",
		AllowedTools:   []string{"CAPABILITY_READ"},
		ForbiddenTools: []string{"capability_write"},
	}

	delegate := NewSubagentDelegate(nil, nil, t.TempDir(), nil, 0, 0).WithCapabilities(registry, nil)
	tools := delegate.buildToolSet(def, NewToolExecutor(nil, t.TempDir(), nil, 0))

	if len(tools) != 1 {
		t.Fatalf("expected 1 capability tool, got %d: %+v", len(tools), tools)
	}
	if tools[0].Name != "capability_read" {
		t.Fatalf("expected capability_read, got %q", tools[0].Name)
	}
}

func TestSubagentDelegate_CapabilityToolExecutesScopedRegistryFirst(t *testing.T) {
	t.Parallel()

	global := capabilities.NewRegistry()
	global.Register("global", fakeCapabilityCallAdapter{
		name:  "global",
		tools: []gateway.ToolDefinition{{Name: "capability_tool"}},
	})
	scoped := capabilities.NewRegistry()
	scoped.Register("scoped", fakeCapabilityCallAdapter{
		name:  "scoped",
		tools: []gateway.ToolDefinition{{Name: "capability_tool"}},
	})

	def := SubagentDefinition{
		Name:         "cap-agent",
		Purpose:      "use capabilities",
		AllowedTools: []string{"capability_tool"},
	}
	delegate := NewSubagentDelegate(nil, nil, t.TempDir(), nil, 0, 0).WithCapabilities(global, scoped)
	call := gateway.ToolCall{
		ID: "cap-call",
		Function: gateway.ToolCallFunction{
			Name:      "capability_tool",
			Arguments: `{"id":"scoped"}`,
		},
	}

	out := delegate.executeTool(context.Background(), call, def, NewToolExecutor(nil, t.TempDir(), nil, 0))
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v out=%s", err, out)
	}
	args, _ := payload["args"].(map[string]any)
	if args["id"] != "scoped" {
		t.Fatalf("expected scoped capability call args, got %#v", payload)
	}
	if payload["adapter"] != "scoped" {
		t.Fatalf("expected scoped adapter, got %#v", payload)
	}
}
