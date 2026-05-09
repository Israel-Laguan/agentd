package kanban

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"agentd/internal/models"
)

func TestAgentProfileLifecycle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seed := models.AgentProfile{
		ID: "default", Name: "Default", Provider: "openai", Model: "gpt-4o-mini",
		Temperature: 0.2, Role: "CODE_GEN", MaxTokens: 1024,
		SystemPrompt: sql.NullString{String: "Be concise.", Valid: true},
	}
	if err := store.UpsertAgentProfile(ctx, seed); err != nil {
		t.Fatalf("seed default: %v", err)
	}
	custom := models.AgentProfile{
		ID: "researcher", Name: "Researcher", Provider: "ollama", Model: "llama3",
		Temperature: 0.7, Role: "RESEARCH", MaxTokens: 2048,
	}
	if err := store.UpsertAgentProfile(ctx, custom); err != nil {
		t.Fatalf("seed researcher: %v", err)
	}

	got, err := store.GetAgentProfile(ctx, "researcher")
	if err != nil {
		t.Fatalf("get researcher: %v", err)
	}
	if got.Role != "RESEARCH" || got.MaxTokens != 2048 || got.Provider != "ollama" {
		t.Fatalf("got = %+v", got)
	}

	list, err := store.ListAgentProfiles(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
}

func TestDeleteAgentProfileGuards(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: "default", Name: "Default", Provider: "openai", Model: "gpt-4o-mini",
	}); err != nil {
		t.Fatalf("seed default: %v", err)
	}
	if err := store.DeleteAgentProfile(ctx, "default"); !errors.Is(err, models.ErrAgentProfileProtected) {
		t.Fatalf("delete default err = %v, want ErrAgentProfileProtected", err)
	}
	if err := store.DeleteAgentProfile(ctx, "missing"); !errors.Is(err, models.ErrAgentProfileNotFound) {
		t.Fatalf("delete missing err = %v, want ErrAgentProfileNotFound", err)
	}

	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: "qa", Name: "QA", Provider: "openai", Model: "gpt-4o",
	}); err != nil {
		t.Fatalf("seed qa: %v", err)
	}
	_, tasks, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if _, err := store.AssignTaskAgent(ctx, tasks[0].ID, tasks[0].UpdatedAt, "qa"); err != nil {
		t.Fatalf("assign qa: %v", err)
	}
	if err := store.DeleteAgentProfile(ctx, "qa"); !errors.Is(err, models.ErrAgentProfileInUse) {
		t.Fatalf("delete in-use err = %v, want ErrAgentProfileInUse", err)
	}
}

func TestAssignTaskAgentRejectsRunningAndUnknownAgent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: "default", Name: "Default", Provider: "openai", Model: "gpt-4o-mini",
	}); err != nil {
		t.Fatalf("seed default: %v", err)
	}
	_, _, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 10)
	if err != nil || len(claimed) == 0 {
		t.Fatalf("claim ready: tasks=%d err=%v", len(claimed), err)
	}
	task := claimed[0]
	if _, err := store.AssignTaskAgent(ctx, task.ID, task.UpdatedAt, "missing"); !errors.Is(err, models.ErrAgentProfileNotFound) {
		t.Fatalf("unknown agent err = %v, want ErrAgentProfileNotFound", err)
	}

	running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 12345)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if _, err := store.AssignTaskAgent(ctx, running.ID, running.UpdatedAt, "default"); !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("running assign err = %v, want ErrStateConflict", err)
	}
}
