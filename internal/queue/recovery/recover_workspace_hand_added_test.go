package recovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// T-037: what the opt-in reset does to files an operator put in a start-empty
// workspace by hand. The decision is document-only (docs/architecture/
// recovery-rerun.md, "Opt-in workspace reset"): the reset cannot tell an
// operator's file from the interrupted attempt's, because the only record of the
// workspace is the started_empty bit. These two tests pin both sides of that
// boundary so neither side can move silently.

// handAdd drops a file into the workspace the way an operator does: straight on
// disk, not through the daemon.
func (f *resetFixture) handAdd(t *testing.T) string {
	t.Helper()
	path := filepath.Join(f.ws.ProjectDir(f.projectID), "operator-notes.txt")
	if err := os.WriteFile(path, []byte("added by hand"), 0o644); err != nil {
		t.Fatalf("write hand-added file: %v", err)
	}
	return path
}

// An untracked hand-added file is deleted. This is the documented hazard, so the
// test names it; if a guard is ever added, this test is the one to change on
// purpose.
func TestBootReconcile_resetDeletesAHandAddedFileWhenWorkspaceReadyWasNotCalled(t *testing.T) {
	f := newResetFixture(t, true, "a")
	handAdded := f.handAdd(t)

	if err := f.boot(t, WithWorkspaceReset(f.ws)); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	if _, err := os.Stat(handAdded); !os.IsNotExist(err) {
		t.Fatalf("hand-added file stat error = %v, want it deleted: the reset has no way to tell it from the "+
			"interrupted attempt's output. If this now survives, a guard was added; update recovery-rerun.md", err)
	}
}

// The sanctioned way to keep hand-seeded content is POST /workspace/ready, which
// clears started_empty. With the bit cleared the same file must survive and the
// recovered task must be refused loudly rather than re-run on a dirty workspace.
func TestBootReconcile_resetKeepsAHandAddedFileOnceWorkspaceReadyClearedTheBit(t *testing.T) {
	f := newResetFixture(t, true, "a")
	handAdded := f.handAdd(t)
	if err := f.store.ClearProjectStartedEmpty(context.Background(), f.projectID); err != nil {
		t.Fatalf("ClearProjectStartedEmpty: %v", err)
	}

	if err := f.boot(t, WithWorkspaceReset(f.ws)); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	if _, err := os.Stat(handAdded); err != nil {
		t.Fatalf("hand-added file stat error = %v, want it kept once started_empty is cleared", err)
	}
	f.assertRefused(t, f.taskIDs[0])
}
