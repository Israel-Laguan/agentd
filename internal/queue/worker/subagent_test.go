package worker

import (
	"context"
	"encoding/json"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// ---------------------------------------------------------------------------
// subagentMockGateway — minimal AIGateway for testing subagent delegation
// ---------------------------------------------------------------------------

type subagentMockGateway struct {
	responses []gateway.AIResponse
	callIdx   int
}

func (m *subagentMockGateway) Generate(_ context.Context, _ gateway.AIRequest) (gateway.AIResponse, error) {
	if m.callIdx >= len(m.responses) {
		return gateway.AIResponse{Content: "done"}, nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func (m *subagentMockGateway) GeneratePlan(_ context.Context, _ string) (*models.DraftPlan, error) {
	return nil, nil
}

func (m *subagentMockGateway) AnalyzeScope(_ context.Context, _ string) (*spec.ScopeAnalysis, error) {
	return nil, nil
}

func (m *subagentMockGateway) ClassifyIntent(_ context.Context, _ string) (*spec.IntentAnalysis, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// SubagentDelegate — tool restriction tests
// ---------------------------------------------------------------------------

func TestSubagentDelegate_ForbiddenToolsNotAvailable(t *testing.T) {
	t.Parallel()

	gw := &subagentMockGateway{
		responses: []gateway.AIResponse{
			{
				ToolCalls: []gateway.ToolCall{
					{ID: "1", Type: "function", Function: gateway.ToolCallFunction{
						Name:      "bash",
						Arguments: `{"command":"echo hello"}`,
					}},
				},
			},
			{Content: "task complete"},
		},
	}

	def := SubagentDefinition{
		Name:           "test-agent",
		Purpose:        "test purpose",
		AllowedTools:   []string{"read"},
		ForbiddenTools: []string{"bash"},
	}

	delegate := NewSubagentDelegate(gw, nil, t.TempDir(), nil, 0, 0)
	result, err := delegate.Delegate(context.Background(), def, "do something", "", "", 0.2, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != SubagentStatusSuccess {
		t.Fatalf("expected success, got %s: %s", result.Status, result.Error)
	}
}

func TestSubagentDelegate_AllowedToolsOnly(t *testing.T) {
	t.Parallel()

	def := SubagentDefinition{
		Name:         "restricted",
		Purpose:      "only read",
		AllowedTools: []string{"read"},
	}

	delegate := NewSubagentDelegate(nil, nil, t.TempDir(), nil, 0, 0)
	tools := delegate.buildToolSet(def, NewToolExecutor(nil, t.TempDir(), nil, 0))

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "read" {
		t.Fatalf("expected tool 'read', got %q", tools[0].Name)
	}
}

func TestSubagentDelegate_ForbiddenToolsFiltered(t *testing.T) {
	t.Parallel()

	def := SubagentDefinition{
		Name:           "no-write",
		Purpose:        "cannot write",
		ForbiddenTools: []string{"write"},
	}

	delegate := NewSubagentDelegate(nil, nil, t.TempDir(), nil, 0, 0)
	tools := delegate.buildToolSet(def, NewToolExecutor(nil, t.TempDir(), nil, 0))

	for _, tool := range tools {
		if tool.Name == "write" {
			t.Fatal("write tool should be filtered out")
		}
	}
	if len(tools) != 2 { // bash, read
		t.Fatalf("expected 2 tools (bash, read), got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// SubagentDelegate — context isolation
// ---------------------------------------------------------------------------

func TestSubagentDelegate_ContextIsolation(t *testing.T) {
	t.Parallel()

	gw := &subagentMockGateway{
		responses: []gateway.AIResponse{
			{Content: "subagent internal reasoning: secret stuff\nFinal answer: 42"},
		},
	}

	def := SubagentDefinition{
		Name:    "isolated",
		Purpose: "answer questions",
	}

	delegate := NewSubagentDelegate(gw, nil, t.TempDir(), nil, 0, 0)
	result, err := delegate.Delegate(context.Background(), def, "what is the answer?", "", "", 0.2, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The result contains the output but the parent never sees the internal messages
	if result.Output == "" {
		t.Fatal("expected non-empty output")
	}
	if result.Status != SubagentStatusSuccess {
		t.Fatalf("expected success, got %s", result.Status)
	}
}

// ---------------------------------------------------------------------------
// SubagentDelegate — depth enforcement
// ---------------------------------------------------------------------------

func TestSubagentDelegate_DepthExceeded(t *testing.T) {
	t.Parallel()

	def := SubagentDefinition{
		Name:    "nested",
		Purpose: "should fail",
	}

	// depth=1 means we've already delegated once — can't go deeper
	delegate := NewSubagentDelegate(nil, nil, t.TempDir(), nil, 0, 1)
	_, err := delegate.Delegate(context.Background(), def, "do stuff", "", "", 0.2, 0)
	if err != ErrDepthExceeded {
		t.Fatalf("expected ErrDepthExceeded, got %v", err)
	}
}

func TestSubagentDelegate_DepthZeroAllowed(t *testing.T) {
	t.Parallel()

	gw := &subagentMockGateway{
		responses: []gateway.AIResponse{
			{Content: "done"},
		},
	}

	def := SubagentDefinition{
		Name:    "root",
		Purpose: "should work",
	}

	delegate := NewSubagentDelegate(gw, nil, t.TempDir(), nil, 0, 0)
	result, err := delegate.Delegate(context.Background(), def, "do stuff", "", "", 0.2, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != SubagentStatusSuccess {
		t.Fatalf("expected success, got %s", result.Status)
	}
}

// ---------------------------------------------------------------------------
// SubagentResult — structured fields
// ---------------------------------------------------------------------------

func TestSubagentResult_StructuredFields(t *testing.T) {
	t.Parallel()

	sb := &fakeSandbox{
		result: sandbox.Result{Success: true, Stdout: "output"},
	}

	gw := &subagentMockGateway{
		responses: []gateway.AIResponse{
			{
				ToolCalls: []gateway.ToolCall{
					{ID: "1", Type: "function", Function: gateway.ToolCallFunction{
						Name:      "bash",
						Arguments: `{"command":"ls"}`,
					}},
				},
			},
			{
				ToolCalls: []gateway.ToolCall{
					{ID: "2", Type: "function", Function: gateway.ToolCallFunction{
						Name:      "write",
						Arguments: `{"path":"out.txt","content":"hello"}`,
					}},
				},
			},
			{Content: "all done"},
		},
	}

	def := SubagentDefinition{
		Name:    "full",
		Purpose: "do everything",
	}

	dir := t.TempDir()
	delegate := NewSubagentDelegate(gw, sb, dir, nil, 0, 0)
	result, err := delegate.Delegate(context.Background(), def, "run tasks", "", "", 0.2, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != SubagentStatusSuccess {
		t.Fatalf("expected success, got %s: %s", result.Status, result.Error)
	}
	if result.Output != "all done" {
		t.Fatalf("expected output 'all done', got %q", result.Output)
	}
	if result.Iterations != 3 {
		t.Fatalf("expected 3 iterations, got %d", result.Iterations)
	}
	if len(result.FilesModified) != 1 || result.FilesModified[0] != "out.txt" {
		t.Fatalf("expected FilesModified=[out.txt], got %v", result.FilesModified)
	}
	if len(result.ToolsCalled) < 2 {
		t.Fatalf("expected at least 2 tools called, got %v", result.ToolsCalled)
	}

	// Verify JSON marshaling roundtrips
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}
	var decoded SubagentResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if decoded.Status != SubagentStatusSuccess {
		t.Fatalf("decoded status mismatch: %s", decoded.Status)
	}
}

// ---------------------------------------------------------------------------
// SubagentDelegate — parallel delegation
// ---------------------------------------------------------------------------

func TestSubagentDelegate_Parallel(t *testing.T) {
	t.Parallel()

	gw := &subagentMockGateway{
		responses: []gateway.AIResponse{
			{Content: "result 1"},
			{Content: "result 2"},
		},
	}

	tasks := []ParallelTask{
		{
			Definition:  SubagentDefinition{Name: "a", Purpose: "first"},
			Description: "task a",
		},
		{
			Definition:  SubagentDefinition{Name: "b", Purpose: "second"},
			Description: "task b",
		},
	}

	delegate := NewSubagentDelegate(gw, nil, t.TempDir(), nil, 0, 0)
	results := delegate.DelegateParallel(context.Background(), tasks, "", "", 0.2, 0)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Status != SubagentStatusSuccess {
			t.Fatalf("task %d failed: %s", i, r.Error)
		}
	}
}

// ---------------------------------------------------------------------------
// DelegateToolDefinition
// ---------------------------------------------------------------------------

func TestDelegateToolDefinition(t *testing.T) {
	t.Parallel()

	def := DelegateToolDefinition()
	if def.Name != "delegate" {
		t.Fatalf("expected name 'delegate', got %q", def.Name)
	}
	if def.Parameters == nil {
		t.Fatal("expected parameters, got nil")
	}
	if len(def.Parameters.Required) != 2 {
		t.Fatalf("expected 2 required params, got %d", len(def.Parameters.Required))
	}
}

// ---------------------------------------------------------------------------
// isToolForbidden
// ---------------------------------------------------------------------------

func TestIsToolForbidden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		def      SubagentDefinition
		want     bool
	}{
		{"no restrictions", "bash", SubagentDefinition{}, false},
		{"forbidden", "bash", SubagentDefinition{ForbiddenTools: []string{"bash"}}, true},
		{"not forbidden", "read", SubagentDefinition{ForbiddenTools: []string{"bash"}}, false},
		{"allowed only", "read", SubagentDefinition{AllowedTools: []string{"read"}}, false},
		{"not allowed", "bash", SubagentDefinition{AllowedTools: []string{"read"}}, true},
		{"both: forbidden wins", "bash", SubagentDefinition{AllowedTools: []string{"bash"}, ForbiddenTools: []string{"bash"}}, true},
		{"case insensitive forbidden", "bash", SubagentDefinition{ForbiddenTools: []string{"Bash"}}, true},
		{"case insensitive allowed", "Read", SubagentDefinition{AllowedTools: []string{"read"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isToolForbidden(tt.toolName, tt.def)
			if got != tt.want {
				t.Fatalf("isToolForbidden(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
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

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type fakeSandbox struct {
	result sandbox.Result
}

func (f *fakeSandbox) Execute(_ context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	return f.result, nil
}

func isErrorJSON(s string) bool {
	var payload map[string]string
	if err := json.Unmarshal([]byte(s), &payload); err != nil {
		return false
	}
	_, ok := payload["error"]
	return ok
}
