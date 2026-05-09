package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func (s *memoryScenario) taskWithEvents(_ context.Context, count int) error {
	events := make([]models.Event, count)
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
	s.store.events = events
	return nil
}

func (s *memoryScenario) gatewayAvailable(context.Context) error {
	summary := memorySummary{Symptom: "ran tests", Solution: "all passed"}
	summaryJSON, _ := json.Marshal(summary)
	s.gw.response = string(summaryJSON)
	s.breaker.open = false
	return nil
}

func (s *memoryScenario) breakerOpen(context.Context) error {
	s.breaker.open = true
	return nil
}

func (s *memoryScenario) curateTask(context.Context) error {
	s.lib.HomeDir = s.homeDir
	_ = s.lib.CurateTask(context.Background(), s.task)
	return nil
}

func (s *memoryScenario) logArchivedEmitted(context.Context) error {
	for _, e := range s.sink.events {
		if e.Type == "LOG_ARCHIVED" {
			return nil
		}
	}
	return fmt.Errorf("missing LOG_ARCHIVED event")
}

func (s *memoryScenario) noLogArchivedEmitted(context.Context) error {
	for _, e := range s.sink.events {
		if e.Type == "LOG_ARCHIVED" {
			return fmt.Errorf("unexpected LOG_ARCHIVED event")
		}
	}
	return nil
}

func (s *memoryScenario) memoryRecordedWithSummary(context.Context) error {
	if s.store.recordedMemory == nil {
		return fmt.Errorf("no memory recorded")
	}
	if !s.store.recordedMemory.Symptom.Valid || s.store.recordedMemory.Symptom.String == "" {
		return fmt.Errorf("memory missing symptom")
	}
	if !s.store.recordedMemory.Solution.Valid || s.store.recordedMemory.Solution.String == "" {
		return fmt.Errorf("memory missing solution")
	}
	return nil
}

func (s *memoryScenario) memoryRecordedWithFallback(context.Context) error {
	if s.store.recordedMemory == nil {
		return fmt.Errorf("no memory recorded")
	}
	if !s.store.recordedMemory.Symptom.Valid {
		return fmt.Errorf("fallback memory missing symptom")
	}
	return nil
}

func (s *memoryScenario) memoryIngestedEmitted(context.Context) error {
	for _, e := range s.sink.events {
		if e.Type == "MEMORY_INGESTED" {
			return nil
		}
	}
	return fmt.Errorf("missing MEMORY_INGESTED event")
}

func (s *memoryScenario) eventsCurated(context.Context) error {
	if !s.store.curated {
		return fmt.Errorf("events not marked as curated")
	}
	return nil
}

func (s *memoryScenario) noMemoryRecorded(context.Context) error {
	if s.store.recordedMemory != nil {
		return fmt.Errorf("unexpected memory recorded")
	}
	return nil
}

func (s *memoryScenario) cleanAndPurge(context.Context) error {
	purged := []PurgedArchive{{ProjectID: "proj-1", TaskID: "task-1"}}
	return s.lib.PurgeCuratedEvents(context.Background(), purged)
}

func (s *memoryScenario) eventsPurgedEmitted(context.Context) error {
	for _, e := range s.sink.events {
		if e.Type == "EVENTS_PURGED" {
			return nil
		}
	}
	return fmt.Errorf("missing EVENTS_PURGED event")
}

func (s *memoryScenario) gatewayReturnsEmpty(context.Context) error {
	summary := memorySummary{Symptom: "", Solution: ""}
	summaryJSON, _ := json.Marshal(summary)
	s.gw.response = string(summaryJSON)
	s.breaker.open = false
	return nil
}

func (s *memoryScenario) gatewayReturnsJunk(_ context.Context, symptom, solution string) error {
	summary := memorySummary{Symptom: symptom, Solution: solution}
	summaryJSON, _ := json.Marshal(summary)
	s.gw.response = string(summaryJSON)
	s.breaker.open = false
	return nil
}

func (s *memoryScenario) memoryDiscardedEmitted(context.Context) error {
	for _, e := range s.sink.events {
		if e.Type == "MEMORY_DISCARDED" {
			return nil
		}
	}
	return fmt.Errorf("missing MEMORY_DISCARDED event")
}

type memStore struct {
	models.KanbanStore
	events         []models.Event
	recordedMemory *models.Memory
	curated        bool
}

func (s *memStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return s.events, nil
}

func (s *memStore) RecordMemory(_ context.Context, m models.Memory) error {
	s.recordedMemory = &m
	return nil
}

func (s *memStore) MarkEventsCurated(context.Context, string) error {
	s.curated = true
	return nil
}

func (s *memStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (s *memStore) GetProject(_ context.Context, _ string) (*models.Project, error) {
	return &models.Project{}, nil
}
func (s *memStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}
func (s *memStore) TouchMemories(context.Context, []string) error             { return nil }
func (s *memStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (s *memStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}
func (s *memStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}

func (s *memStore) GetSetting(_ context.Context, _ string) (string, bool, error) {
	return "", false, nil
}

type memGateway struct {
	response string
	err      error
}

func (g *memGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if g.err != nil {
		return gateway.AIResponse{}, g.err
	}
	if req.JSONMode && !strings.Contains(g.response, "{") {
		return gateway.AIResponse{Content: `{"symptom":"ran tests","solution":"all passed"}`}, nil
	}
	return gateway.AIResponse{Content: g.response}, nil
}

func (g *memGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *memGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *memGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

type memBreaker struct{ open bool }

func (b *memBreaker) IsOpen() bool { return b.open }

type memSink struct{ events []models.Event }

func (s *memSink) Emit(_ context.Context, e models.Event) error {
	s.events = append(s.events, e)
	return nil
}
