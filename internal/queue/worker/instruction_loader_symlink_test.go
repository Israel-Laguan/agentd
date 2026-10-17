package worker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectInstructions_SymlinkOutsideWorkspaceNotRead(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("# Rules\n- leaked host secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	l := &InstructionLoader{}
	pi, err := l.LoadProjectInstructions(ws, "")
	if err == nil && pi != nil {
		t.Fatalf("instructions read through a symlink that leaves the workspace: %+v", pi)
	}
}

func TestLoadProjectInstructions_SymlinkInsideWorkspaceStillRead(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "real.md"), []byte("# Rules\n- local rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", filepath.Join(ws, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	l := &InstructionLoader{}
	pi, err := l.LoadProjectInstructions(ws, "")
	if err != nil || pi == nil {
		t.Fatalf("in-workspace symlink should load: pi=%v err=%v", pi, err)
	}
}
