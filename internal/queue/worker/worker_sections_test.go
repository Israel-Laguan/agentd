package worker

import (
	"context"
	"database/sql"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func sectionsOf(msgs []gateway.PromptMessage) []gateway.Section {
	out := make([]gateway.Section, len(msgs))
	for i, m := range msgs {
		out[i] = m.Section
	}
	return out
}

func TestAgenticSeedMessagesAreTaggedBySection(t *testing.T) {
	w := &Worker{retriever: &mockMemoryRetriever{memories: []models.Memory{{
		Scope:    "LESSON",
		Symptom:  sql.NullString{String: "Flaky test", Valid: true},
		Solution: sql.NullString{String: "Add retry", Valid: true},
	}}}}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, Title: "Fix bug", Description: "Fix it"}
	msgs := w.AssembleAgenticSystemPrompt(context.Background(), task, models.Project{}, models.AgentProfile{})
	want := []gateway.Section{gateway.SectionSystem, gateway.SectionTask, gateway.SectionMemories}
	got := sectionsOf(msgs)
	if len(got) != len(want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sections = %v, want %v", got, want)
		}
	}
}
