package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

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
