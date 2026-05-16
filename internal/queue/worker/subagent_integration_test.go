package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/gateway"
)

func TestSubagentDelegate_NestedDelegation(t *testing.T) {
	workspace := t.TempDir()
	subagentDir := filepath.Join(workspace, ".agentd", "subagents")
	if err := os.MkdirAll(subagentDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Nested definition
	nestedContent := `# Subagent: nested

## Purpose

do the actual work
`
	if err := os.WriteFile(filepath.Join(subagentDir, "nested.md"), []byte(nestedContent), 0644); err != nil {
		t.Fatalf("write nested def: %v", err)
	}

	gw := &subagentMockGateway{
		responses: []gateway.AIResponse{
			{
				ToolCalls: []gateway.ToolCall{
					{ID: "1", Type: "function", Function: gateway.ToolCallFunction{
						Name:      "delegate",
						Arguments: `{"subagent":"nested","task":"do nested work"}`,
					}},
				},
			},
			{Content: "nested done"},
			{Content: "parent complete"},
		},
	}

	parentDef := SubagentDefinition{
		Name:         "parent",
		Purpose:      "delegate to nested",
		AllowedTools: []string{"delegate"},
	}

	delegate := NewSubagentDelegate(gw, nil, workspace, nil, 0, 0).withMaxDelegationDepth(2)
	result, err := delegate.Delegate(context.Background(), parentDef, "parent task", "", "", 0.2, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != SubagentStatusSuccess {
		t.Fatalf("expected success, got %s: %s", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "parent complete") {
		t.Errorf("expected output to contain 'parent complete', got: %s", result.Output)
	}

	requests := gw.requestSnapshot()
	if len(requests) != 3 {
		t.Fatalf("expected 3 gateway calls (parent, nested, parent-final), got %d", len(requests))
	}
}

func TestSubagentDelegate_ExecuteToolDepthExceeded(t *testing.T) {
	t.Parallel()

	delegate := NewSubagentDelegate(nil, nil, t.TempDir(), nil, 0, 0)
	toolExec := NewToolExecutor(nil, t.TempDir(), nil, 0)

	def := SubagentDefinition{
		Name:         "test",
		Purpose:      "test",
		AllowedTools: []string{"delegate"},
	}

	call := gateway.ToolCall{
		ID:   "1",
		Type: "function",
		Function: gateway.ToolCallFunction{
			Name:      "delegate",
			Arguments: `{"subagent":"nested","task":"do something"}`,
		},
	}

	result := delegate.executeTool(context.Background(), call, def, toolExec)
	if !strings.Contains(result, "depth exceeded") {
		t.Errorf("expected depth exceeded error, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// executeDelegate — worker integration
// ---------------------------------------------------------------------------

func TestWorker_ExecuteDelegate_MissingSubagent(t *testing.T) {
	t.Parallel()

	w := &Worker{}
	dir := t.TempDir()
	toolExec := NewToolExecutor(nil, dir, nil, 0)

	call := gateway.ToolCall{
		ID:   "1",
		Type: "function",
		Function: gateway.ToolCallFunction{
			Name:      "delegate",
			Arguments: `{"subagent":"nonexistent","task":"do something"}`,
		},
	}

	result := w.executeDelegate(context.Background(), call, toolExec)
	if !isErrorJSON(result) {
		t.Fatalf("expected error JSON, got %q", result)
	}
}

func TestWorker_ExecuteDelegate_EmptyArgs(t *testing.T) {
	t.Parallel()

	w := &Worker{}
	toolExec := NewToolExecutor(nil, t.TempDir(), nil, 0)

	call := gateway.ToolCall{
		ID:   "1",
		Type: "function",
		Function: gateway.ToolCallFunction{
			Name:      "delegate",
			Arguments: `{"subagent":"","task":""}`,
		},
	}

	result := w.executeDelegate(context.Background(), call, toolExec)
	if !isErrorJSON(result) {
		t.Fatalf("expected error JSON for empty args, got %q", result)
	}
}

func TestWorker_DispatchTool_DelegateSuccess(t *testing.T) {
	t.Parallel()

	dir := writeSubagentDefinition(t, "helper", `# Subagent: helper

## Purpose

Help with a bounded task.
`)
	gw := &subagentMockGateway{
		responses: []gateway.AIResponse{
			{
				Content: strings.Repeat("hidden intermediate ", 8),
				ToolCalls: []gateway.ToolCall{
					{ID: "1", Type: "function", Function: gateway.ToolCallFunction{
						Name:      "read",
						Arguments: `{"path":"missing.txt"}`,
					}},
				},
			},
			{Content: "final answer"},
		},
	}
	w := &Worker{gateway: gw}
	toolExec := NewToolExecutor(nil, dir, nil, 0)

	result := w.DispatchTool(context.Background(), "session", gateway.ToolCall{
		ID:   "delegate-call",
		Type: "function",
		Function: gateway.ToolCallFunction{
			Name:      "delegate",
			Arguments: `{"subagent":"helper","task":"do helper work"}`,
		},
	}, nil, toolExec)

	if strings.Contains(result, "hidden intermediate") {
		t.Fatalf("parent-visible delegate result leaked subagent transcript: %s", result)
	}
	var payload SubagentResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("invalid delegate JSON: %v out=%s", err, result)
	}
	if payload.Status != SubagentStatusSuccess || payload.Output != "final answer" {
		t.Fatalf("unexpected delegate result: %+v", payload)
	}
}

func TestWorker_ExecuteDelegateParallel(t *testing.T) {
	t.Parallel()

	dir := writeSubagentDefinition(t, "helper", `# Subagent: helper

## Purpose

Help with bounded tasks.
`)
	w := &Worker{gateway: subagentTaskGateway{}}
	toolExec := NewToolExecutor(nil, dir, nil, 0)

	result := w.executeDelegateParallel(context.Background(), gateway.ToolCall{
		ID:   "parallel-call",
		Type: "function",
		Function: gateway.ToolCallFunction{
			Name: "delegate_parallel",
			Arguments: `{"tasks":[` +
				`{"subagent":"helper","task":"first task"},` +
				`{"subagent":"helper","task":"second task"}` +
				`]}`,
		},
	}, toolExec, nil)

	var payload []SubagentResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("invalid delegate_parallel JSON: %v out=%s", err, result)
	}
	if len(payload) != 2 {
		t.Fatalf("expected 2 results, got %d", len(payload))
	}
	if payload[0].Output != "first" || payload[1].Output != "second" {
		t.Fatalf("results not in input order: %+v", payload)
	}
}

func TestWorker_ExecuteDelegateParallel_InvalidArgs(t *testing.T) {
	t.Parallel()

	w := &Worker{}
	toolExec := NewToolExecutor(nil, t.TempDir(), nil, 0)
	result := w.executeDelegateParallel(context.Background(), gateway.ToolCall{
		ID:   "parallel-call",
		Type: "function",
		Function: gateway.ToolCallFunction{
			Name:      "delegate_parallel",
			Arguments: `{"tasks":[]}`,
		},
	}, toolExec, nil)
	if !isErrorJSON(result) {
		t.Fatalf("expected error JSON for empty task list, got %q", result)
	}
}
