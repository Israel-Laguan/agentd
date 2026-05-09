package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// --- fakes ---

type fakeStore struct {
	models.KanbanStore
	events          []models.Event
	recordedMemory  *models.Memory
	curated         bool
	deletedCurated  bool
	project         *models.Project
}

func (s *fakeStore) ListEventsByTask(_ context.Context, _ string) ([]models.Event, error) {
	return s.events, nil
}

func (s *fakeStore) RecordMemory(_ context.Context, m models.Memory) error {
	s.recordedMemory = &m
	return nil
}

func (s *fakeStore) MarkEventsCurated(_ context.Context, _ string) error {
	s.curated = true
	return nil
}

func (s *fakeStore) DeleteCuratedEvents(_ context.Context, _ string) error {
	s.deletedCurated = true
	return nil
}

func (s *fakeStore) GetProject(_ context.Context, _ string) (*models.Project, error) {
	if s.project != nil {
		return s.project, nil
	}
	return &models.Project{}, nil
}

func (s *fakeStore) RecallMemories(_ context.Context, _ models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}

func (s *fakeStore) TouchMemories(_ context.Context, _ []string) error { return nil }

func (s *fakeStore) SupersedeMemories(_ context.Context, _ []string, _ string) error { return nil }

func (s *fakeStore) ListUnsupersededMemories(_ context.Context) ([]models.Memory, error) {
	return nil, nil
}

func (s *fakeStore) ListMemories(_ context.Context, _ models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}

func (s *fakeStore) GetSetting(_ context.Context, _ string) (string, bool, error) {
	return "", false, nil
}

type fakeGateway struct {
	response string
	err      error
}

func (g *fakeGateway) Generate(_ context.Context, _ gateway.AIRequest) (gateway.AIResponse, error) {
	if g.err != nil {
		return gateway.AIResponse{}, g.err
	}
	return gateway.AIResponse{Content: g.response}, nil
}

func (g *fakeGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *fakeGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *fakeGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

type fakeBreaker struct{ open bool }

func (b *fakeBreaker) IsOpen() bool { return b.open }

type fakeSink struct{ events []models.Event }

func (s *fakeSink) Emit(_ context.Context, e models.Event) error {
	s.events = append(s.events, e)
	return nil
}

// --- tests ---

func testTask() models.Task {
	return models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1"},
		ProjectID:  "proj-1",
	}
}

func testEvents(n int) []models.Event {
	events := make([]models.Event, n)
	for i := range events {
		events[i] = models.Event{
			BaseEntity: models.BaseEntity{
				ID:        fmt.Sprintf("evt-%d", i),
				CreatedAt: time.Date(2025, 6, 1, 12, 0, i, 0, time.UTC),
			},
			ProjectID: "proj-1",
			TaskID:    sql.NullString{String: "task-1", Valid: true},
			Type:      "LOG_CHUNK",
			Payload:   fmt.Sprintf("line %d output", i),
		}
	}
	return events
}

func TestChunkLog(t *testing.T) {
	text := strings.Repeat("a", 100) + "\n\n" + strings.Repeat("b", 100) + "\n\n" + strings.Repeat("c", 50)
	chunks := chunkLog(text, 120)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for _, ch := range chunks {
		if len(ch) > 120 {
			t.Errorf("chunk exceeds maxChars: len=%d", len(ch))
		}
	}
}

func TestChunkLog_SingleChunk(t *testing.T) {
	text := "short log"
	chunks := chunkLog(text, 1000)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestHeadTailChars(t *testing.T) {
	s := "0123456789"
	if got := headChars(s, 5); got != "01234" {
		t.Errorf("headChars = %q", got)
	}
	if got := tailChars(s, 5); got != "56789" {
		t.Errorf("tailChars = %q", got)
	}
	if got := headChars(s, 100); got != s {
		t.Error("headChars should return full string when n > len")
	}
	if got := tailChars(s, 100); got != s {
		t.Error("tailChars should return full string when n > len")
	}
}

func TestCurateTask_MapReduce(t *testing.T) {
	longPayload := strings.Repeat("x", 500)
	events := make([]models.Event, 10)
	for i := range events {
		events[i] = models.Event{
			BaseEntity: models.BaseEntity{
				ID:        fmt.Sprintf("evt-%d", i),
				CreatedAt: time.Date(2025, 6, 1, 12, 0, i, 0, time.UTC),
			},
			ProjectID: "proj-1",
			TaskID:    sql.NullString{String: "task-1", Valid: true},
			Type:      "LOG_CHUNK",
			Payload:   longPayload,
		}
	}

	store := &fakeStore{events: events}
	sink := &fakeSink{}
	summary := memorySummary{Symptom: "big task", Solution: "completed"}
	summaryJSON, _ := json.Marshal(summary)

	// The gateway always returns a short summary for map/reduce calls, then
	// the final call returns the JSON extract. We use a countingGateway that
	// returns the JSON on its last call.
	lib := &Librarian{
		Store:   store,
		Gateway: &extractOnFinalGateway{extractJSON: string(summaryJSON), mapResponse: "short"},
		Breaker: &fakeBreaker{open: false},
		Sink:    sink,
		Cfg:     config.LibrarianConfig{ChunkChars: 1000, MaxReducePasses: 3, FallbackHeadTailChars: 100, ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatalf("CurateTask: %v", err)
	}
	if store.recordedMemory == nil {
		t.Fatal("expected memory")
	}
	if store.recordedMemory.Symptom.String != "big task" {
		t.Errorf("symptom = %q", store.recordedMemory.Symptom.String)
	}
}

// extractOnFinalGateway returns mapResponse for all calls, except when JSONMode
// is true it returns the extractJSON string (the final extraction call).
type extractOnFinalGateway struct {
	extractJSON string
	mapResponse string
}

func (g *extractOnFinalGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if req.JSONMode {
		return gateway.AIResponse{Content: g.extractJSON}, nil
	}
	return gateway.AIResponse{Content: g.mapResponse}, nil
}

func (g *extractOnFinalGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *extractOnFinalGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *extractOnFinalGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}
