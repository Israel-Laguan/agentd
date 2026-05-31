package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
)

func TestDispatchToolWithHooks_WritesStructuredAudit(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-dispatch.jsonl")
	w := NewWorker(nil, nil, &fakeSuccessExecutor{}, nil, nil, WorkerOptions{
		MaxToolIterations: 5,
		Audit: config.AuditConfig{
			Enabled: true,
			Path:    path,
		},
	})

	call := gateway.ToolCall{
		ID: "call-audit-1",
		Function: gateway.ToolCallFunction{
			Name:      "bash",
			Arguments: `{"command":"echo audit"}`,
		},
	}
	_, _ = w.dispatchToolWithHooks(
		context.Background(), "sess-d", "proj-d", "sess-d:0", time.Now(),
		call, nil, w.toolExecutor, nil, nil, "openai",
	)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if strings.Contains(string(data), "echo audit") {
		t.Fatal("raw args must not appear in structured audit log")
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec["record_type"] != "tool_dispatch" {
		t.Fatalf("record_type = %v, want %q", rec["record_type"], "tool_dispatch")
	}
	if rec["type"] != "tool_dispatch" {
		t.Fatalf("type = %v, want %q", rec["type"], "tool_dispatch")
	}
	if rec["task_id"] != "sess-d" {
		t.Fatalf("task_id = %v, want sess-d", rec["task_id"])
	}
	if rec["project_id"] != "proj-d" {
		t.Fatalf("project_id = %v, want proj-d", rec["project_id"])
	}
	if rec["provider"] != "openai" {
		t.Fatalf("provider = %v, want openai", rec["provider"])
	}
	if _, ok := rec["token_usage"]; !ok {
		t.Fatal("missing token_usage")
	}
}

func filepathJoinTemp(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}
