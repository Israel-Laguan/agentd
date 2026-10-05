package services_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

// unreadableSource builds a source_path that passes ValidateSourcePath but cannot
// be copied: it is a directory, so it validates, and one file in it is mode 000,
// so the copy fails partway. Root ignores file modes, which makes the failure
// impossible to stage, so the test skips there rather than pass vacuously.
func unreadableSource(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: file modes do not stop the copy, so a mid-copy failure cannot be staged")
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a-readable.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(src, "b-locked.txt")
	if err := os.WriteFile(locked, []byte("secret"), 0o000); err != nil {
		t.Fatal(err)
	}
	return src
}

// B-021 (B-004 follow-up): validating source_path up front rejects a bad path
// before anything is persisted, but a copy that fails partway used to happen
// after the project and task rows were committed, leaving a project with PENDING
// tasks and a half-populated workspace that nothing can unlock.
func TestMaterializeWithFailingSeedCopyPersistsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wsRoot := t.TempDir()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(wsRoot)
	svc := services.NewProjectService(store, &sandbox.FSWorkspaceManager{Root: wsRoot})

	_, _, err := svc.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "seed-fails",
		SourcePath:  unreadableSource(t),
		Tasks:       []models.DraftTask{{Title: "T1", Description: "work"}},
	})
	if err == nil {
		t.Fatal("MaterializePlan() error = nil, want the copy failure")
	}

	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("projects after a failed seed = %d (%q), want none: the failure orphaned a project", len(projects), projects[0].Name)
	}
	entries, err := os.ReadDir(wsRoot)
	if err != nil {
		t.Fatalf("ReadDir(root) error = %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("workspace root after a failed seed holds %v, want it empty (no half-copied project dir, no staging dir)", names)
	}
}

// The copy is staged before the project exists and swapped in after, so a
// successful seed must look exactly as before: content in the project's own
// directory, nothing left behind in the root.
func TestMaterializeWithSourcePathLeavesNoStagingDirectory(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("world"), 0o640); err != nil {
		t.Fatal(err)
	}
	wsRoot := t.TempDir()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(wsRoot)
	svc := services.NewProjectService(store, &sandbox.FSWorkspaceManager{Root: wsRoot})

	project, _, err := svc.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "seed-ok", SourcePath: src, Tasks: []models.DraftTask{{Title: "T1", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	entries, err := os.ReadDir(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != project.ID {
		t.Fatalf("workspace root entries = %v, want only the project dir %s", entries, project.ID)
	}
	info, err := os.Stat(filepath.Join(project.WorkspacePath, "hello.txt"))
	if err != nil {
		t.Fatalf("seeded file: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("seeded file mode = %v, want the source's 0640 preserved", info.Mode().Perm())
	}
}

// failingEnsureWorkspace is the real manager with EnsureProjectDir broken, the
// one failure left between staging the copy and swapping it in.
type failingEnsureWorkspace struct{ *sandbox.FSWorkspaceManager }

func (failingEnsureWorkspace) EnsureProjectDir(context.Context, string) (string, error) {
	return "", os.ErrPermission
}

// A copy that finished must not outlive a materialize that then fails: the
// staged directory is the one thing the failure path has to clean up itself.
func TestMaterializeDiscardsTheStagedSeedWhenALaterStepFails(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	wsRoot := t.TempDir()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(wsRoot)
	svc := services.NewProjectService(store, failingEnsureWorkspace{&sandbox.FSWorkspaceManager{Root: wsRoot}})

	_, _, err := svc.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "ensure-fails", SourcePath: src, Tasks: []models.DraftTask{{Title: "T1", Description: "d"}},
	})
	if err == nil {
		t.Fatal("MaterializePlan() error = nil, want the EnsureProjectDir failure")
	}
	entries, err := os.ReadDir(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace root holds %v after a failed materialize, want the staged copy discarded", entries)
	}
}
