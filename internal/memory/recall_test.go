package memory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
)

// -- Test 2: Namespace Isolation --

type recallStore struct {
	models.KanbanStore
	memories []models.Memory
	touched  []string
}

func (s *recallStore) RecallMemories(_ context.Context, q models.RecallQuery) ([]models.Memory, error) {
	var out []models.Memory
	for _, m := range s.memories {
		if m.SupersededBy.Valid {
			continue
		}
		isGlobal := m.Scope == "GLOBAL"
		isCurrentProject := q.ProjectID != "" && m.ProjectID.Valid && m.ProjectID.String == q.ProjectID
		isPref := m.Scope == "USER_PREFERENCE" && q.UserID != ""
		isOtherProject := m.ProjectID.Valid && m.ProjectID.String != q.ProjectID
		if isOtherProject && !isGlobal && !isPref {
			continue
		}
		if isGlobal || isCurrentProject || isPref {
			out = append(out, m)
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (s *recallStore) TouchMemories(_ context.Context, ids []string) error {
	s.touched = append(s.touched, ids...)
	return nil
}

func TestRetriever_NamespaceIsolation(t *testing.T) {
	store := &recallStore{
		memories: []models.Memory{
			{ID: "global-1", Scope: "GLOBAL", Symptom: sql.NullString{String: "global rule", Valid: true}, Solution: sql.NullString{String: "do X", Valid: true}},
			{ID: "proj1-1", Scope: "TASK_CURATION", ProjectID: sql.NullString{String: "project_1", Valid: true}, Symptom: sql.NullString{String: "proj1 rule", Valid: true}, Solution: sql.NullString{String: "do Y", Valid: true}},
			{ID: "proj2-1", Scope: "TASK_CURATION", ProjectID: sql.NullString{String: "project_2", Valid: true}, Symptom: sql.NullString{String: "proj2 rule", Valid: true}, Solution: sql.NullString{String: "do Z", Valid: true}},
		},
	}

	r := &Retriever{Store: store, Cfg: config.LibrarianConfig{RecallTimeout: 5 * time.Second, RecallTopK: 10}}

	results := r.Recall(context.Background(), "some intent", "project_2", "")
	if len(results) == 0 {
		t.Fatal("expected results for project_2 recall")
	}

	for _, m := range results {
		if m.ProjectID.Valid && m.ProjectID.String == "project_1" {
			t.Errorf("project_1 memory should NOT appear in project_2 recall, got %q", m.ID)
		}
	}

	hasGlobal := false
	hasProj2 := false
	for _, m := range results {
		if m.Scope == "GLOBAL" {
			hasGlobal = true
		}
		if m.ProjectID.Valid && m.ProjectID.String == "project_2" {
			hasProj2 = true
		}
	}
	if !hasGlobal {
		t.Error("expected GLOBAL memories in recall")
	}
	if !hasProj2 {
		t.Error("expected project_2 memories in recall")
	}
}

// -- Test 3: Slow DB Fallback --

type hangingStore struct {
	models.KanbanStore
}

func (s *hangingStore) RecallMemories(ctx context.Context, _ models.RecallQuery) ([]models.Memory, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return []models.Memory{{ID: "should-not-return"}}, nil
	}
}

func (s *hangingStore) TouchMemories(context.Context, []string) error { return nil }

func TestRetriever_TimeoutFallback(t *testing.T) {
	r := &Retriever{
		Store: &hangingStore{},
		Cfg:   config.LibrarianConfig{RecallTimeout: 50 * time.Millisecond, RecallTopK: 5},
	}

	start := time.Now()
	results := r.Recall(context.Background(), "test intent", "", "")
	elapsed := time.Since(start)

	if len(results) != 0 {
		t.Errorf("expected no results on timeout, got %d", len(results))
	}
	if elapsed > 1*time.Second {
		t.Errorf("recall took %s, should return within 1s", elapsed)
	}
}

func TestRetriever_NilRetriever(t *testing.T) {
	var r *Retriever
	results := r.Recall(context.Background(), "test", "", "")
	if results != nil {
		t.Error("nil retriever should return nil")
	}
}

func TestFormatLessons(t *testing.T) {
	memories := []models.Memory{
		{Scope: "TASK_CURATION", Symptom: sql.NullString{String: "test failed", Valid: true}, Solution: sql.NullString{String: "fix config", Valid: true}},
		{Scope: "USER_PREFERENCE", Solution: sql.NullString{String: "use tabs", Valid: true}},
	}
	lessons := FormatLessons(memories)
	if lessons == "" {
		t.Error("expected non-empty lessons")
	}
	prefs := FormatPreferences(memories)
	if prefs == "" {
		t.Error("expected non-empty preferences")
	}
}
