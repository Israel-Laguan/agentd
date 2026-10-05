package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func stagedFixture(t *testing.T) (*FSWorkspaceManager, StagedSeed) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &FSWorkspaceManager{Root: t.TempDir()}
	staged, err := m.StageSeed(context.Background(), src)
	if err != nil {
		t.Fatalf("StageSeed() error = %v", err)
	}
	return m, staged
}

func rootEntries(t *testing.T, m *FSWorkspaceManager) []string {
	t.Helper()
	entries, err := os.ReadDir(m.Root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Promote replaces the empty project directory and never overwrites content: a
// workspace that already holds a file is refused and left exactly as it was.
func TestStagedSeedPromoteRefusesAPopulatedWorkspace(t *testing.T) {
	m, staged := stagedFixture(t)
	t.Cleanup(staged.Discard)
	dir, err := m.EnsureProjectDir(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "mine.txt")
	if err := os.WriteFile(existing, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := staged.Promote(context.Background(), "p1"); err == nil {
		t.Fatal("Promote() error = nil, want a refusal: the workspace already holds a file")
	}
	if data, err := os.ReadFile(existing); err != nil || string(data) != "mine" {
		t.Fatalf("existing file = (%q, %v), want it untouched", data, err)
	}
}

func TestStagedSeedPromoteThenDiscardLeavesTheWorkspace(t *testing.T) {
	m, staged := stagedFixture(t)
	if _, err := m.EnsureProjectDir(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}

	if err := staged.Promote(context.Background(), "p1"); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	staged.Discard()
	staged.Discard()

	if data, err := os.ReadFile(filepath.Join(m.ProjectDir("p1"), "seed.txt")); err != nil || string(data) != "seed" {
		t.Fatalf("seeded file = (%q, %v), want it present after Discard", data, err)
	}
	if got := rootEntries(t, m); len(got) != 1 || got[0] != "p1" {
		t.Fatalf("root entries = %v, want only p1 (no staging directory)", got)
	}
	if err := staged.Promote(context.Background(), "p1"); err == nil {
		t.Fatal("second Promote() error = nil, want it refused")
	}
}

func TestStagedSeedDiscardRemovesTheStagingDirectory(t *testing.T) {
	m, staged := stagedFixture(t)
	if got := rootEntries(t, m); len(got) != 1 {
		t.Fatalf("root entries = %v, want exactly the staging directory", got)
	}
	staged.Discard()
	if got := rootEntries(t, m); len(got) != 0 {
		t.Fatalf("root entries = %v after Discard, want none", got)
	}
}
