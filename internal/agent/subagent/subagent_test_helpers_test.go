package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	agentcontext "agentd/internal/agent/context"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

var NewToolExecutor = agenttools.NewToolExecutor

func totalChars(messages []gateway.PromptMessage) int {
	return agentcontext.TotalChars(messages)
}

func stripToolErrorPrefix(s string) string {
	return agenttools.StripToolErrorPrefix(s)
}

type fakeCapabilityAdapter struct {
	tools []gateway.ToolDefinition
}

func (f fakeCapabilityAdapter) Name() string { return "fake" }

func (f fakeCapabilityAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityAdapter) CallTool(context.Context, string, map[string]any) (any, error) {
	return "ok", nil
}

func (f fakeCapabilityAdapter) Close() error { return nil }

type fakeCapabilityCallAdapter struct {
	name  string
	tools []gateway.ToolDefinition
}

func (f fakeCapabilityCallAdapter) Name() string { return f.name }

func (f fakeCapabilityCallAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityCallAdapter) CallTool(_ context.Context, name string, args map[string]any) (any, error) {
	return map[string]any{"adapter": f.name, "tool": name, "args": args}, nil
}

func (f fakeCapabilityCallAdapter) Close() error { return nil }

type fakeCapabilityErrorAdapter struct {
	name  string
	tools []gateway.ToolDefinition
	err   error
}

func (f fakeCapabilityErrorAdapter) Name() string { return f.name }

func (f fakeCapabilityErrorAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityErrorAdapter) CallTool(context.Context, string, map[string]any) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return nil, fmt.Errorf("capability failed")
}

func (f fakeCapabilityErrorAdapter) Close() error { return nil }

// ---------------------------------------------------------------------------
// subagentMockGateway — minimal AIGateway for testing subagent delegation
// ---------------------------------------------------------------------------

type subagentMockGateway struct {
	responses []gateway.AIResponse
	requests  []gateway.AIRequest
	callIdx   int
	mu        sync.Mutex
}

func (m *subagentMockGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	if m.callIdx >= len(m.responses) {
		return gateway.AIResponse{Content: "done"}, nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func (m *subagentMockGateway) requestSnapshot() []gateway.AIRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]gateway.AIRequest(nil), m.requests...)
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
func (m *subagentMockGateway) Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.NoopEmbed(ctx, req)
}

type subagentTaskGateway struct{}

func (subagentTaskGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if len(req.Messages) == 0 {
		return gateway.AIResponse{Content: "missing task"}, nil
	}
	task := req.Messages[len(req.Messages)-1].Content
	switch task {
	case "first task":
		return gateway.AIResponse{Content: "first"}, nil
	case "second task":
		return gateway.AIResponse{Content: "second"}, nil
	default:
		return gateway.AIResponse{Content: task}, nil
	}
}

func (subagentTaskGateway) GeneratePlan(_ context.Context, _ string) (*models.DraftPlan, error) {
	return nil, nil
}

func (subagentTaskGateway) AnalyzeScope(_ context.Context, _ string) (*spec.ScopeAnalysis, error) {
	return nil, nil
}

func (subagentTaskGateway) ClassifyIntent(_ context.Context, _ string) (*spec.IntentAnalysis, error) {
	return nil, nil
}
func (subagentTaskGateway) Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.NoopEmbed(ctx, req)
}

type fakeSandbox struct {
	result sandbox.Result
}

func (f *fakeSandbox) Execute(_ context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	return f.result, nil
}

func isErrorJSON(s string) bool {
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(s)), &payload); err != nil {
		return false
	}
	_, ok := payload["error"]
	return ok
}

func writeSubagentDefinition(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	subagentDir := filepath.Join(dir, ".agentd", "subagents")
	if err := os.MkdirAll(subagentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, name+".md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}
