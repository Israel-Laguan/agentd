package memory

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentd/internal/models"
)

// WriteArchive creates <dir>/<projectID>/<taskID>.tar.gz containing one text
// file per event. Returns the path to the written archive.
func WriteArchive(dir, projectID, taskID string, events []models.Event) (string, error) {
	projectDir := filepath.Join(dir, projectID)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return "", fmt.Errorf("create archive project dir: %w", err)
	}
	archivePath := filepath.Join(projectDir, taskID+".tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("create archive file: %w", err)
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()
	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	for _, evt := range events {
		content := fmt.Sprintf("[%s] %s\n%s\n", evt.Type, evt.CreatedAt.Format(time.RFC3339), evt.Payload)
		hdr := &tar.Header{
			Name:    evt.ID + ".txt",
			Size:    int64(len(content)),
			Mode:    0o644,
			ModTime: evt.CreatedAt,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return "", fmt.Errorf("write tar header for event %s: %w", evt.ID, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return "", fmt.Errorf("write tar body for event %s: %w", evt.ID, err)
		}
	}
	return archivePath, nil
}

// PurgedArchive identifies a task whose archive was deleted by grace-period cleanup.
type PurgedArchive struct {
	ProjectID string
	TaskID    string
}

// CleanStaleArchives removes .tar.gz files under dir whose modification time
// is older than graceDays. Returns the list of purged (projectID, taskID) pairs.
func CleanStaleArchives(dir string, graceDays int) ([]PurgedArchive, error) {
	cutoff := time.Now().Add(-time.Duration(graceDays) * 24 * time.Hour)
	var purged []PurgedArchive
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".tar.gz") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			projectDir := filepath.Dir(path)
			projectID := filepath.Base(projectDir)
			taskID := strings.TrimSuffix(d.Name(), ".tar.gz")
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove stale archive %s: %w", path, err)
			}
			purged = append(purged, PurgedArchive{ProjectID: projectID, TaskID: taskID})
		}
		return nil
	})
	return purged, err
}
