package worker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteContextPack_DoesNotWriteThroughSymlink(t *testing.T) {
	ws := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp := NewContextPack("t1", "parent1", "summary", []string{"a.go"}, ContextPackConfig{})
	if err := os.Symlink(victim, filepath.Join(ws, PackFilePath(cp.ParentTaskID, cp.Version))); err != nil {
		t.Fatal(err)
	}

	if err := WriteContextPack(ws, cp); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, _ := os.ReadFile(victim)
	if string(got) != "original" {
		t.Fatalf("pack was written through the planted symlink: %q", got)
	}
	if _, err := ReadContextPack(filepath.Join(ws, PackFilePath(cp.ParentTaskID, cp.Version))); err != nil {
		t.Fatalf("pack should exist in the workspace: %v", err)
	}
}
