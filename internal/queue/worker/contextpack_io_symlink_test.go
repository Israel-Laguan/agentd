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

func TestReadContextPack_DoesNotReadThroughSymlink(t *testing.T) {
	outside := t.TempDir()
	cp := NewContextPack("t1", "parent1", "host secret", []string{"a.go"}, ContextPackConfig{})
	if err := WriteContextPack(outside, cp); err != nil {
		t.Fatal(err)
	}
	name := PackFilePath(cp.ParentTaskID, cp.Version)
	ws := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, name), filepath.Join(ws, name)); err != nil {
		t.Fatal(err)
	}

	if got, err := ReadContextPack(filepath.Join(ws, name)); err == nil {
		t.Fatalf("ReadContextPack followed a symlink out of the workspace: %q", got.Summary)
	}
	if got, err := ReadContextPackWithFallback(filepath.Join(ws, name), ""); err == nil {
		t.Fatalf("ReadContextPackWithFallback followed a symlink out of the workspace: %q", got.Summary)
	}
}
