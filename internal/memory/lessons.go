package memory

import (
	"fmt"
	"os"
	"path/filepath"

	"agentd/internal/models"
)

// appendLessonMarkdown appends a curated memory entry to lessons.md inside the
// project workspace. Failure is non-fatal; the durable memory row in SQLite is
// the authoritative record.
func appendLessonMarkdown(workspace string, task models.Task, m models.Memory) error {
	path := filepath.Join(workspace, "lessons.md")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "## %s (%s)\n", task.Title, task.ID); err != nil {
		return err
	}
	if m.Symptom.Valid {
		if _, err := fmt.Fprintf(f, "- Symptom: %s\n", m.Symptom.String); err != nil {
			return err
		}
	}
	if m.Solution.Valid {
		if _, err := fmt.Fprintf(f, "- Solution: %s\n", m.Solution.String); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(f); err != nil {
		return err
	}
	return nil
}
