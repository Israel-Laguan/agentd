package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/gateway"
)

func TestToolExecutor_UnknownTool_ReturnsValidJSON(t *testing.T) {
	t.Parallel()
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{Name: `weird"name`, Arguments: `{}`},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\nout=%s", err, out)
	}
	if payload["error"] == "" {
		t.Fatal("expected error field")
	}
}

func TestToolExecutor_Read_RejectsOversizedFile(t *testing.T) {
	prev := maxToolReadFileBytes
	maxToolReadFileBytes = 8
	t.Cleanup(func() { maxToolReadFileBytes = prev })

	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(path, []byte("123456789"), 0644); err != nil {
		t.Fatal(err)
	}

	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "big.txt"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error, got %q", out)
	}
}
