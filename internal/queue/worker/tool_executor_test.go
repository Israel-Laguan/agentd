package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
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

type fakeSuccessExecutor struct{}

func (fakeSuccessExecutor) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	return sandbox.Result{
		Success: true,
		Stdout:  "hello world",
		ExitCode: 0,
	}, nil
}

func TestToolExecutor_Bash_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := NewToolExecutor(fakeSuccessExecutor{}, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameBash,
			Arguments: `{"command": "cat test.txt"}`,
		},
	})
	if out != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", out)
	}
}

func TestToolExecutor_Bash_ValidationFailure(t *testing.T) {
	t.Parallel()
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameBash,
			Arguments: `{}`,
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

type fakeFailingExecutor struct{}

func (fakeFailingExecutor) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	return sandbox.Result{}, errors.New("sandbox execution failed")
}

func TestToolExecutor_Bash_SandboxFailure(t *testing.T) {
	t.Parallel()
	ex := NewToolExecutor(fakeFailingExecutor{}, t.TempDir(), nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameBash,
			Arguments: `{"command": "echo test"}`,
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

func TestToolExecutor_Read_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "myfile.txt")
	if err := os.WriteFile(path, []byte("file content here"), 0644); err != nil {
		t.Fatal(err)
	}

	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "myfile.txt"}`,
		},
	})
	if out != "file content here" {
		t.Fatalf("expected %q, got %q", "file content here", out)
	}
}

func TestToolExecutor_Read_ValidationFailure(t *testing.T) {
	t.Parallel()
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{}`,
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

func TestToolExecutor_Read_PathJail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "../../../etc/passwd"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error for path escape, got %q", out)
	}
}

func TestToolExecutor_Write_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameWrite,
			Arguments: `{"path": "newfile.txt", "content": "written content"}`,
		},
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] != nil {
		t.Fatalf("unexpected error: %v", payload["error"])
	}

	content, err := os.ReadFile(filepath.Join(dir, "newfile.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "written content" {
		t.Fatalf("expected %q, got %q", "written content", string(content))
	}
}

func TestToolExecutor_Write_ValidationFailure(t *testing.T) {
	t.Parallel()
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)

	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameWrite,
			Arguments: `{}`,
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

func TestToolExecutor_Write_PathJail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameWrite,
			Arguments: `{"path": "../../../tmp/evil.txt", "content": "malicious"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error for path escape, got %q", out)
	}
}
