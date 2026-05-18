package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

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
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\nout=%s", err, out)
	}
	if payload["error"] == "" {
		t.Fatal("expected error field")
	}
}

func TestToolExecutor_Read_RejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(path, []byte("123456789"), 0644); err != nil {
		t.Fatal(err)
	}

	ex := NewToolExecutor(nil, dir, nil, 0)
	ex.maxReadBytes = 8
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "big.txt"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error, got %q", out)
	}
}

type fakeSuccessExecutor struct{}

func (fakeSuccessExecutor) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	return sandbox.Result{
		Success:  true,
		Stdout:   "hello world",
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
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
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

func TestToolExecutor_Bash_PathJail(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workspace, err := (&sandbox.FSWorkspaceManager{Root: root}).EnsureProjectDir(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	sb := &sandbox.BashExecutor{Root: root, Inactivity: time.Second}
	ex := NewToolExecutor(sb, workspace, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameBash,
			Arguments: `{"command": "cat ../../../etc/passwd"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\nout=%s", err, out)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error for path escape, got %q", out)
	}
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
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
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

func TestToolExecutor_Read_ErrorShapedFileContent_ClassifiedAsSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := `{"error":"cached api failure"}`
	path := filepath.Join(dir, "response.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ex := NewToolExecutor(nil, dir, nil, 0)
	raw := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "response.json"}`,
		},
	})
	if raw != content {
		t.Fatalf("raw = %q, want file bytes %q", raw, content)
	}
	tr := classifyBuiltinToolResult("c1", toolNameRead, raw, 0)
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("Status = %s, want success for error-shaped file content", tr.Status)
	}
	if tr.ForContext() != content {
		t.Fatalf("ForContext() = %q, want raw content without [ERROR] prefix", tr.ForContext())
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
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error, got %q", out)
	}
}

func TestToolExecutor_Read_RejectsFIFO(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("FIFO not supported on Windows")
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}

	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "pipe"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error for FIFO, got %q", out)
	}
	if !strings.Contains(payload["error"], "not a regular file") {
		t.Fatalf("expected not a regular file error, got %q", payload["error"])
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
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
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
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
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
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error, got %q", out)
	}
}

func TestToolExecutor_Write_ValidationFailure_MissingContent(t *testing.T) {
	t.Parallel()
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameWrite,
			Arguments: `{"path":"foo.txt"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error, got %q", out)
	}
	if !strings.Contains(payload["error"], "content") {
		t.Fatalf("expected error to mention content, got %q", payload["error"])
	}
}

func TestToolExecutor_Write_AllowsEmptyContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameWrite,
			Arguments: `{"path":"empty.txt","content":""}`,
		},
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] != nil {
		t.Fatalf("unexpected error: %v", payload["error"])
	}
	content, err := os.ReadFile(filepath.Join(dir, "empty.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatalf("expected empty file, got %q", content)
	}
}

func TestToolExecutor_Read_CancelledContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(ctx, gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "file.txt"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected cancellation error, got %q", out)
	}
}

func TestDispatchTool_SchemaValidation_UnknownArgument(t *testing.T) {
	t.Parallel()
	mockSB := &fakeSuccessExecutor{}
	executor := NewToolExecutor(mockSB, t.TempDir(), nil, 0)
	w := NewWorker(nil, nil, mockSB, nil, nil, WorkerOptions{})

	tr := w.DispatchTool(context.Background(), "test-session", gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameBash,
			Arguments: `{"command":"echo hi","extra":"val"}`,
		},
	}, nil, executor)
	if tr.Status != ToolStatusVetoed {
		t.Fatalf("expected schema veto, got status %s content %q", tr.Status, tr.Content)
	}
	if strings.Contains(tr.Content, "hello world") {
		t.Fatal("tool should not have executed after schema veto")
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
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error for path escape, got %q", out)
	}
}

func TestToolExecutor_Read_PathJail_Symlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameRead,
			Arguments: `{"path": "link"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error for symlink escape, got %q", out)
	}
}

func TestToolExecutor_Write_PathJail_SymlinkParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("untouched"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(dir, "evil")); err != nil {
		t.Fatal(err)
	}

	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameWrite,
			Arguments: `{"path": "evil/new.txt", "content": "pwned"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error for symlink parent escape, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file was created or modified via symlink parent: %v", err)
	}
	content, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "untouched" {
		t.Fatalf("outside file was modified: %q", content)
	}
}

func TestToolExecutor_Write_PathJail_Symlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	ex := NewToolExecutor(nil, dir, nil, 0)
	out := ex.Execute(context.Background(), gateway.ToolCall{
		Function: gateway.ToolCallFunction{
			Name:      toolNameWrite,
			Arguments: `{"path": "link", "content": "pwned"}`,
		},
	})
	var payload map[string]string
	if err := json.Unmarshal([]byte(stripToolErrorPrefix(out)), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error for symlink escape, got %q", out)
	}
	content, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Fatalf("outside file was modified: %q", content)
	}
}
