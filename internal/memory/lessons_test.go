package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

func TestCurateTaskWritesLessonsMarkdown(t *testing.T) {
	workspace := t.TempDir()
	store := &fakeStore{
		events:  testEvents(3),
		project: &models.Project{BaseEntity: models.BaseEntity{ID: "proj-1"}, WorkspacePath: workspace},
	}
	sink := &fakeSink{}
	summary := memorySummary{Symptom: "build failed", Solution: "install gcc"}
	summaryJSON, _ := json.Marshal(summary)

	lib := &Librarian{
		Store:   store,
		Gateway: &extractOnFinalGateway{mapResponse: "summary", extractJSON: string(summaryJSON)},
		Breaker: &fakeBreaker{open: false},
		Sink:    sink,
		Cfg:     config.LibrarianConfig{ChunkChars: 50000, MaxReducePasses: 3, FallbackHeadTailChars: 2000, ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1"},
		ProjectID:  "proj-1",
		Title:      "Compile",
	}

	if err := lib.CurateTask(context.Background(), task); err != nil {
		t.Fatalf("CurateTask() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workspace, "lessons.md"))
	if err != nil {
		t.Fatalf("read lessons.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "## Compile (task-1)") {
		t.Errorf("missing task header in lessons.md:\n%s", content)
	}
	if !strings.Contains(content, "- Symptom: build failed") {
		t.Errorf("missing symptom in lessons.md:\n%s", content)
	}
	if !strings.Contains(content, "- Solution: install gcc") {
		t.Errorf("missing solution in lessons.md:\n%s", content)
	}
}

func TestLessonsMarkdownAppendsToExistingFile(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "lessons.md")
	if err := os.WriteFile(path, []byte("# Existing\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	task := models.Task{BaseEntity: models.BaseEntity{ID: "t2"}, Title: "Deploy"}
	mem := models.Memory{
		Symptom:  sql.NullString{String: "timeout", Valid: true},
		Solution: sql.NullString{String: "increase limit", Valid: true},
	}
	if err := appendLessonMarkdown(workspace, task, mem); err != nil {
		t.Fatalf("appendLessonMarkdown() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "# Existing") {
		t.Error("original content was overwritten")
	}
	if !strings.Contains(content, "## Deploy (t2)") {
		t.Errorf("appended content missing:\n%s", content)
	}
}
