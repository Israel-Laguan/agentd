package memory

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestWriteArchive(t *testing.T) {
	dir := t.TempDir()
	events := sampleArchiveEvents()

	archivePath, err := WriteArchive(dir, "proj-1", "task-1", events)
	if err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	if !strings.HasSuffix(archivePath, ".tar.gz") {
		t.Fatalf("expected .tar.gz suffix, got %s", archivePath)
	}

	assertArchiveContents(t, archivePath)
}

func sampleArchiveEvents() []models.Event {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	return []models.Event{
		{
			BaseEntity: models.BaseEntity{ID: "evt-1", CreatedAt: now},
			ProjectID:  "proj-1",
			TaskID:     sql.NullString{String: "task-1", Valid: true},
			Type:       "LOG_CHUNK",
			Payload:    "hello world",
		},
		{
			BaseEntity: models.BaseEntity{ID: "evt-2", CreatedAt: now.Add(time.Second)},
			ProjectID:  "proj-1",
			TaskID:     sql.NullString{String: "task-1", Valid: true},
			Type:       "RESULT",
			Payload:    "success",
		},
	}
}

func assertArchiveContents(t *testing.T, archivePath string) {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer func() { _ = f.Close() }()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)

	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		names = append(names, hdr.Name)
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar file body: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "LOG_CHUNK") && !strings.Contains(content, "RESULT") {
			t.Errorf("unexpected content in %s: %s", hdr.Name, content)
		}
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 files in archive, got %d: %v", len(names), names)
	}
}

func TestWriteArchive_EmptyEvents(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteArchive(dir, "proj", "task", nil)
	if err != nil {
		t.Fatalf("WriteArchive with no events: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected non-zero file even with no events")
	}
}

func TestCleanStaleArchives(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "proj-1")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	stalePath := filepath.Join(projectDir, "old.tar.gz")
	freshPath := filepath.Join(projectDir, "new.tar.gz")
	for _, p := range []string{stalePath, freshPath} {
		if err := os.WriteFile(p, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staleTime := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	_, err := CleanStaleArchives(dir, 7)
	if err != nil {
		t.Fatalf("CleanStaleArchives: %v", err)
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Error("stale archive should have been removed")
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Error("fresh archive should still exist")
	}
}

func TestCleanStaleArchives_NoDir(t *testing.T) {
	_, err := CleanStaleArchives(filepath.Join(t.TempDir(), "nonexistent"), 7)
	if err == nil {
		t.Error("expected error for missing directory")
	}
}
