package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

func TestCurateTask_SystemProjectSkipsLessonWrite(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	temp := t.TempDir()
	if err := os.Chdir(temp); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		events:  testEvents(1),
		project: &models.Project{BaseEntity: models.BaseEntity{ID: "_system"}, WorkspacePath: "_system"},
	}
	lib := &Librarian{Store: store, Gateway: &fakeGateway{}, Breaker: &fakeBreaker{}, HomeDir: t.TempDir(), ProjectsRoot: filepath.Join(temp, "projects")}

	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(temp, "_system", "lessons.md")); !os.IsNotExist(err) {
		t.Fatalf("system lesson file should not exist, stat error = %v", err)
	}
}

func TestCurateTask_SmallLog(t *testing.T) {
	store := &fakeStore{events: testEvents(3)}
	sink := &fakeSink{}
	summary := memorySummary{Symptom: "ran tests", Solution: "all passed"}
	summaryJSON, _ := json.Marshal(summary)

	gw := &extractOnFinalGateway{
		mapResponse: "chunk summary: ran tests and all passed",
		extractJSON: string(summaryJSON),
	}

	lib := &Librarian{
		Store:   store,
		Gateway: gw,
		Breaker: &fakeBreaker{open: false},
		Sink:    sink,
		Cfg:     config.LibrarianConfig{ChunkChars: 50000, MaxReducePasses: 3, FallbackHeadTailChars: 2000, ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatalf("CurateTask: %v", err)
	}

	if store.recordedMemory == nil {
		t.Fatal("expected memory to be recorded")
	}
	if store.recordedMemory.Scope != "TASK_CURATION" {
		t.Errorf("scope = %q, want TASK_CURATION", store.recordedMemory.Scope)
	}
	if !store.recordedMemory.Symptom.Valid || store.recordedMemory.Symptom.String != "ran tests" {
		t.Errorf("symptom = %v", store.recordedMemory.Symptom)
	}
	if !store.curated {
		t.Error("events should be marked curated")
	}

	var logArchived, memIngested bool
	for _, e := range sink.events {
		switch e.Type {
		case "LOG_ARCHIVED":
			logArchived = true
		case "MEMORY_INGESTED":
			memIngested = true
		}
	}
	if !logArchived {
		t.Error("expected LOG_ARCHIVED event")
	}
	if !memIngested {
		t.Error("expected MEMORY_INGESTED event")
	}
}

func TestCurateTask_FallbackOnBreakerOpen(t *testing.T) {
	events := testEvents(5)
	store := &fakeStore{events: events}
	sink := &fakeSink{}

	lib := &Librarian{
		Store:   store,
		Gateway: &fakeGateway{},
		Breaker: &fakeBreaker{open: true},
		Sink:    sink,
		Cfg:     config.LibrarianConfig{ChunkChars: 50000, MaxReducePasses: 3, FallbackHeadTailChars: 100, ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatalf("CurateTask: %v", err)
	}

	if store.recordedMemory == nil {
		t.Fatal("expected memory via fallback")
	}
	if !store.recordedMemory.Symptom.Valid || store.recordedMemory.Symptom.String == "" {
		t.Error("fallback should have set symptom from head")
	}
	if !store.recordedMemory.Solution.Valid || store.recordedMemory.Solution.String == "" {
		t.Error("fallback should have set solution from tail")
	}
}

func TestCurateTask_FallbackOnLLMError(t *testing.T) {
	store := &fakeStore{events: testEvents(3)}
	sink := &fakeSink{}
	gw := &fakeGateway{err: errors.New("llm down")}

	lib := &Librarian{
		Store:   store,
		Gateway: gw,
		Breaker: &fakeBreaker{open: false},
		Sink:    sink,
		Cfg:     config.LibrarianConfig{ChunkChars: 50000, MaxReducePasses: 3, FallbackHeadTailChars: 100, ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatalf("CurateTask: %v", err)
	}
	if store.recordedMemory == nil {
		t.Fatal("expected memory via fallback on LLM error")
	}
}

func TestCurateTask_NoEvents(t *testing.T) {
	store := &fakeStore{events: nil}
	lib := &Librarian{
		Store:   store,
		Cfg:     config.LibrarianConfig{ChunkChars: 8000},
		HomeDir: t.TempDir(),
	}
	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatalf("CurateTask with no events: %v", err)
	}
	if store.recordedMemory != nil {
		t.Error("should not record memory for zero events")
	}
}
