package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkspaceFile_RequiresPath(t *testing.T) {
	t.Parallel()
	for _, rel := range []string{"", "."} {
		_, err := ResolveWorkspaceFile(t.TempDir(), rel)
		if !errors.Is(err, ErrPathRequired) {
			t.Fatalf("ResolveWorkspaceFile(%q) error = %v, want %v", rel, err, ErrPathRequired)
		}
	}
}

func TestResolveWorkspaceFile_RejectsAbsolutePath(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	absolute := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(absolute, []byte("inside"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveWorkspaceFile(workspace, absolute)
	if !errors.Is(err, ErrAbsolutePathNotAllowed) {
		t.Fatalf("ResolveWorkspaceFile absolute error = %v, want %v", err, ErrAbsolutePathNotAllowed)
	}
}

func TestResolveWorkspaceFile_RejectsEscape(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	parent := filepath.Dir(workspace)
	outside := filepath.Join(parent, "outside_resolve_test.txt")
	if err := os.WriteFile(outside, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	_, err := ResolveWorkspaceFile(workspace, "../outside_resolve_test.txt")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveWorkspaceFile_RejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveWorkspaceFile(workspace, "link.txt")
	if !errors.Is(err, ErrPathEscapesWorkspace) {
		t.Fatalf("ResolveWorkspaceFile symlink escape error = %v, want %v", err, ErrPathEscapesWorkspace)
	}
}

func TestResolveWorkspaceFile_ResolvesInside(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	rel := "inner.txt"
	if err := os.WriteFile(filepath.Join(workspace, rel), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	full, err := ResolveWorkspaceFile(workspace, rel)
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

func TestResolveWorkspaceFileWithRoot_SkipsRootEval(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	root, err := EvalWorkspaceRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	rel := "inner.txt"
	if err := os.WriteFile(filepath.Join(workspace, rel), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	full, err := ResolveWorkspaceFileWithRoot(workspace, root, rel)
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
