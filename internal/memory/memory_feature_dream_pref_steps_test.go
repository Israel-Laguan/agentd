package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/config"
	"agentd/internal/models"
)

type dreamTestStore struct {
	models.KanbanStore
	memories []models.Memory
}

func (s *dreamTestStore) ListUnsupersededMemories(_ context.Context) ([]models.Memory, error) {
	var out []models.Memory
	for _, m := range s.memories {
		if !m.SupersededBy.Valid {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *dreamTestStore) RecordMemory(_ context.Context, m models.Memory) error {
	if m.ID == "" {
		m.ID = fmt.Sprintf("merged-%d", len(s.memories))
	}
	s.memories = append(s.memories, m)
	return nil
}

func (s *dreamTestStore) SupersedeMemories(_ context.Context, oldIDs []string, newID string) error {
	old := make(map[string]struct{}, len(oldIDs))
	for _, id := range oldIDs {
		old[id] = struct{}{}
	}
	for i := range s.memories {
		if _, ok := old[s.memories[i].ID]; ok {
			s.memories[i].SupersededBy = sql.NullString{String: newID, Valid: true}
		}
	}
	return nil
}

func (s *dreamTestStore) ListMemories(_ context.Context, _ models.MemoryFilter) ([]models.Memory, error) {
	return s.memories, nil
}

func (s *dreamTestStore) GetSetting(_ context.Context, _ string) (string, bool, error) {
	return "", false, nil
}

func (d *dreamScenario) seedMemories(_ context.Context, count int, text string) error {
	for i := 0; i < count; i++ {
		d.store.memories = append(d.store.memories, models.Memory{
			ID:       fmt.Sprintf("orig-%d", i),
			Scope:    "TASK_CURATION",
			Symptom:  sql.NullString{String: text, Valid: true},
			Solution: sql.NullString{String: text, Valid: true},
		})
	}
	return nil
}

func (d *dreamScenario) gatewayReturnsMerged(context.Context) error {
	merged := memorySummary{Symptom: "consolidated CORS rule", Solution: "Add Header A for CORS"}
	mergedJSON, _ := json.Marshal(merged)
	d.gw.response = string(mergedJSON)
	return nil
}

func (d *dreamScenario) runDream(context.Context) error {
	d.dreamer = &DreamAgent{
		Store: d.store, Gateway: d.gw,
		Cfg: config.LibrarianConfig{DreamClusterMinSize: 3, DreamSimilarityThreshold: 0.2},
	}
	return d.dreamer.Run(context.Background())
}

func (d *dreamScenario) unsupersededCount(_ context.Context, expected int) error {
	unsup, _ := d.store.ListUnsupersededMemories(context.Background())
	if len(unsup) != expected {
		return fmt.Errorf("expected %d unsuperseded, got %d", expected, len(unsup))
	}
	return nil
}

func (d *dreamScenario) originalSuperseded(_ context.Context, expected int) error {
	count := 0
	for _, m := range d.store.memories {
		if m.SupersededBy.Valid {
			count++
		}
	}
	if count != expected {
		return fmt.Errorf("expected %d superseded, got %d", expected, count)
	}
	return nil
}

func (p *prefScenario) setPreference(_ context.Context, userID, text string) error {
	p.prefUser = userID
	p.prefText = text
	return nil
}

func (p *prefScenario) storePreference(context.Context) error {
	return p.store.RecordMemory(context.Background(), models.Memory{
		Scope:    "USER_PREFERENCE",
		Tags:     sql.NullString{String: "user_id:" + p.prefUser, Valid: true},
		Symptom:  sql.NullString{String: "preference", Valid: true},
		Solution: sql.NullString{String: p.prefText, Valid: true},
	})
}

func (p *prefScenario) prefMemoryExists(context.Context) error {
	if p.store.recordedMemory == nil {
		return fmt.Errorf("no preference memory recorded")
	}
	if p.store.recordedMemory.Scope != "USER_PREFERENCE" {
		return fmt.Errorf("scope = %q, want USER_PREFERENCE", p.store.recordedMemory.Scope)
	}
	return nil
}

func (p *prefScenario) memoryContains(_ context.Context, text string) error {
	if p.store.recordedMemory == nil {
		return fmt.Errorf("no memory recorded")
	}
	if !strings.Contains(p.store.recordedMemory.Solution.String, text) {
		return fmt.Errorf("memory solution %q does not contain %q", p.store.recordedMemory.Solution.String, text)
	}
	return nil
}
