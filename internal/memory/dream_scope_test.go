package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

func scopedMemory(id string, scope models.MemoryScope, project string) models.Memory {
	return models.Memory{
		ID:        id,
		Scope:     scope,
		ProjectID: sql.NullString{String: project, Valid: project != ""},
		Symptom:   sql.NullString{String: "Fix CORS error by adding Header A", Valid: true},
		Solution:  sql.NullString{String: "Added Header A for CORS", Valid: true},
	}
}

func runDream(t *testing.T, store *dreamStore) {
	t.Helper()
	merged, _ := json.Marshal(memorySummary{Symptom: "CORS errors", Solution: "Add Header A"})
	da := &DreamAgent{
		Store:   store,
		Gateway: &fakeGateway{response: string(merged)},
		Cfg:     config.LibrarianConfig{DreamClusterMinSize: 3, DreamSimilarityThreshold: 0.2},
	}
	if err := da.Run(context.Background()); err != nil {
		t.Fatalf("DreamAgent.Run: %v", err)
	}
}

// B-068: near-identical memories from different projects stay separate.
func TestDreamAgent_DoesNotMergeAcrossProjects(t *testing.T) {
	store := newDreamStore()
	for i := 0; i < 2; i++ {
		store.memories = append(store.memories,
			scopedMemory(fmt.Sprintf("a-%d", i), models.MemoryScopeProject, "proj-a"),
			scopedMemory(fmt.Sprintf("b-%d", i), models.MemoryScopeProject, "proj-b"))
	}
	runDream(t, store)
	if len(store.superseded) != 0 {
		t.Fatalf("no project has 3 memories, yet %d were superseded: %v", len(store.superseded), store.superseded)
	}
}

func TestDreamAgent_DoesNotMergeAcrossScopes(t *testing.T) {
	store := newDreamStore()
	store.memories = append(store.memories,
		scopedMemory("g-0", models.MemoryScopeGlobal, ""),
		scopedMemory("g-1", models.MemoryScopeGlobal, ""),
		scopedMemory("p-0", models.MemoryScopeProject, "proj-a"))
	runDream(t, store)
	if len(store.superseded) != 0 {
		t.Fatalf("mixed scopes merged: %v", store.superseded)
	}
}

// The merged memory keeps its cluster's project and is found by the ID the
// dream agent chose, not by reloading every memory.
func TestDreamAgent_MergedMemoryKeepsProjectAndUsesChosenID(t *testing.T) {
	store := newDreamStore()
	for i := 0; i < 3; i++ {
		store.memories = append(store.memories, scopedMemory(fmt.Sprintf("a-%d", i), models.MemoryScopeProject, "proj-a"))
	}
	runDream(t, store)
	if store.listCalls != 1 {
		t.Errorf("ListUnsupersededMemories called %d times, want 1", store.listCalls)
	}
	var merged models.Memory
	for _, m := range store.memories {
		if m.Tags.String == "dream_consolidated" {
			merged = m
		}
	}
	if merged.ID == "" || merged.ID == "merged-1" || merged.ProjectID.String != "proj-a" {
		t.Fatalf("merged = %+v", merged)
	}
	for _, id := range store.superseded {
		if id != merged.ID {
			t.Errorf("superseded by %q, want %q", id, merged.ID)
		}
	}
}
