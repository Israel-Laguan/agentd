package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/config"
)

func TestCleanStaleArchives_ReturnsPurged(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "proj-1")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	stalePath := filepath.Join(projectDir, "task-old.tar.gz")
	freshPath := filepath.Join(projectDir, "task-new.tar.gz")
	for _, p := range []string{stalePath, freshPath} {
		if err := os.WriteFile(p, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staleTime := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	purged, err := CleanStaleArchives(dir, 7)
	if err != nil {
		t.Fatalf("CleanStaleArchives: %v", err)
	}

	if len(purged) != 1 {
		t.Fatalf("expected 1 purged archive, got %d", len(purged))
	}
	if purged[0].ProjectID != "proj-1" {
		t.Errorf("purged ProjectID = %q", purged[0].ProjectID)
	}
	if purged[0].TaskID != "task-old" {
		t.Errorf("purged TaskID = %q", purged[0].TaskID)
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Error("stale archive should have been removed")
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Error("fresh archive should still exist")
	}
}

func TestLibrarian_PurgeCuratedEvents(t *testing.T) {
	store := &fakeStore{events: testEvents(3)}
	sink := &fakeSink{}

	lib := &Librarian{
		Store:   store,
		Sink:    sink,
		Cfg:     config.LibrarianConfig{ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	purged := []PurgedArchive{
		{ProjectID: "proj-1", TaskID: "task-1"},
	}

	if err := lib.PurgeCuratedEvents(context.Background(), purged); err != nil {
		t.Fatalf("PurgeCuratedEvents: %v", err)
	}

	if !store.deletedCurated {
		t.Error("expected DeleteCuratedEvents to be called")
	}

	var eventsPurged bool
	for _, e := range sink.events {
		if e.Type == "EVENTS_PURGED" {
			eventsPurged = true
		}
	}
	if !eventsPurged {
		t.Error("expected EVENTS_PURGED event")
	}
}
