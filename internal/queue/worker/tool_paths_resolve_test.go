package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkspaceFile_RejectsEscape(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	parent := filepath.Dir(workspace)
	outside := filepath.Join(parent, "outside_resolve_test.txt")
	if err := os.WriteFile(outside, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	_, err := resolveWorkspaceFile(workspace, "../outside_resolve_test.txt")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveWorkspaceFile_ResolvesInside(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	rel := "inner.txt"
	if err := os.WriteFile(filepath.Join(workspace, rel), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	full, err := resolveWorkspaceFile(workspace, rel)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" {
		t.Fatalf("read %q", data)
	}
}
