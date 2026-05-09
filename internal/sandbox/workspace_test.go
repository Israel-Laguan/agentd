package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/models"
)

func TestFSWorkspaceManagerEnsureProjectDir(t *testing.T) {
	root := t.TempDir()
	manager := &FSWorkspaceManager{Root: root}

	got, err := manager.EnsureProjectDir(context.Background(), "project-1")
	if err != nil {
		t.Fatalf("EnsureProjectDir() error = %v", err)
	}
	want, err := filepath.Abs(filepath.Join(root, "project-1"))
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	if got != want {
		t.Fatalf("EnsureProjectDir() = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("workspace not created as dir: info=%v err=%v", info, err)
	}
	if _, err := manager.EnsureProjectDir(context.Background(), "project-1"); err != nil {
		t.Fatalf("EnsureProjectDir() second call error = %v", err)
	}
}

func TestFSWorkspaceManagerProjectDirIsPure(t *testing.T) {
	root := t.TempDir()
	manager := &FSWorkspaceManager{Root: root}
	got := manager.ProjectDir("project-2")

	if got != filepath.Join(root, "project-2") {
		t.Fatalf("ProjectDir() = %q", got)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("ProjectDir touched filesystem: err=%v", err)
	}
}

func TestJailPathRejectsEscape(t *testing.T) {
	root := t.TempDir()
	_, err := JailPath(root, filepath.Join(root, ".."))
	if !errors.Is(err, models.ErrSandboxViolation) {
		t.Fatalf("JailPath() error = %v, want ErrSandboxViolation", err)
	}
}

func TestSecureDeleteRemovesProjectOnly(t *testing.T) {
	root := t.TempDir()
	manager := &FSWorkspaceManager{Root: root}
	dir, err := manager.EnsureProjectDir(context.Background(), "project-1")
	if err != nil {
		t.Fatalf("EnsureProjectDir() error = %v", err)
	}
	if err := manager.SecureDelete(context.Background(), "project-1"); err != nil {
		t.Fatalf("SecureDelete() error = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("project dir still exists: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root should remain: %v", err)
	}
}
