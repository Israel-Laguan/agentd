package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

// -- Test 4: Dreaming Merge (Deduplication) --

type dreamStore struct {
	models.KanbanStore
	memories   []models.Memory
	superseded map[string]string
}

func newDreamStore() *dreamStore {
	return &dreamStore{superseded: make(map[string]string)}
}

func (s *dreamStore) ListUnsupersededMemories(_ context.Context) ([]models.Memory, error) {
	var out []models.Memory
	for _, m := range s.memories {
		if !m.SupersededBy.Valid {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *dreamStore) RecordMemory(_ context.Context, m models.Memory) error {
	if m.ID == "" {
		m.ID = "merged-1"
	}
	s.memories = append(s.memories, m)
	return nil
}

func (s *dreamStore) SupersedeMemories(_ context.Context, oldIDs []string, newID string) error {
	for _, id := range oldIDs {
		s.superseded[id] = newID
		for i := range s.memories {
			if s.memories[i].ID == id {
				s.memories[i].SupersededBy = sql.NullString{String: newID, Valid: true}
			}
		}
	}
	return nil
}

func (s *dreamStore) ListMemories(_ context.Context, _ models.MemoryFilter) ([]models.Memory, error) {
	return s.memories, nil
}

func (s *dreamStore) GetSetting(_ context.Context, _ string) (string, bool, error) {
	return "", false, nil
}

func TestDreamAgent_MergesDuplicates(t *testing.T) {
	store := newDreamStore()
	for i := 0; i < 5; i++ {
		store.memories = append(store.memories, models.Memory{
			ID:       models.Memory{}.ID,
			Scope:    "TASK_CURATION",
			Symptom:  sql.NullString{String: "Fix CORS error by adding Header A", Valid: true},
			Solution: sql.NullString{String: "Added Header A for CORS", Valid: true},
		})
		store.memories[i].ID = string(rune('a'+i)) + "-id"
	}

	merged := memorySummary{Symptom: "CORS errors", Solution: "Add Header A to fix CORS"}
	mergedJSON, _ := json.Marshal(merged)

	da := &DreamAgent{
		Store:   store,
		Gateway: &fakeGateway{response: string(mergedJSON)},
		Cfg: config.LibrarianConfig{
			DreamClusterMinSize:      3,
			DreamSimilarityThreshold: 0.2,
		},
	}

	if err := da.Run(context.Background()); err != nil {
		t.Fatalf("DreamAgent.Run: %v", err)
	}

	unsuperseded, _ := store.ListUnsupersededMemories(context.Background())
	if len(unsuperseded) != 1 {
		t.Fatalf("expected 1 unsuperseded memory after merge, got %d", len(unsuperseded))
	}
	if unsuperseded[0].Symptom.String != "CORS errors" {
		t.Errorf("merged symptom = %q", unsuperseded[0].Symptom.String)
	}

	if len(store.superseded) != 5 {
		t.Errorf("expected 5 superseded memories, got %d", len(store.superseded))
	}
}

func TestDreamAgent_SkipsWhenBreakerOpen(t *testing.T) {
	da := &DreamAgent{
		Breaker: &fakeBreaker{open: true},
	}
	if err := da.Run(context.Background()); err != nil {
		t.Fatalf("expected nil error when breaker open, got %v", err)
	}
}

func TestDreamAgent_NilStore(t *testing.T) {
	da := &DreamAgent{}
	if err := da.Run(context.Background()); err != nil {
		t.Fatalf("expected nil error for nil store, got %v", err)
	}
}

func TestJaccard(t *testing.T) {
	a := map[string]struct{}{"cors": {}, "header": {}, "error": {}}
	b := map[string]struct{}{"cors": {}, "header": {}, "fix": {}}
	j := jaccard(a, b)
	if j < 0.4 || j > 0.6 {
		t.Errorf("jaccard = %f, expected ~0.5", j)
	}

	empty := map[string]struct{}{}
	if jaccard(empty, empty) != 0 {
		t.Error("jaccard of empties should be 0")
	}
}
