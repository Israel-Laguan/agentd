package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
