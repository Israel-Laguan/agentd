package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"agentd/internal/models"
)

// Any profile row still in non-agentic mode is upgraded at startup, operator
// created rows included. Its prompt and tools are left as the operator set them.
func TestSeedUpgradesOperatorCreatedProfile(t *testing.T) {
	ctx := context.Background()
	store, err := openTestStore(t, filepath.Join(t.TempDir(), "global.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()
	ops := models.AgentProfile{
		ID: "ops-bot", Name: "Ops", Role: "CODE_GEN", Temperature: 0.1, MaxTokens: 512,
		SystemPrompt: sql.NullString{String: "Our own prompt.", Valid: true},
	}
	if err := store.UpsertAgentProfile(ctx, ops); err != nil {
		t.Fatalf("UpsertAgentProfile: %v", err)
	}

	if err := seedDefaultAgent(ctx, store, false); err != nil {
		t.Fatalf("seedDefaultAgent: %v", err)
	}
	got, err := store.GetAgentProfile(ctx, "ops-bot")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if !got.AgenticMode {
		t.Fatal("operator-created profile stayed non-agentic after startup")
	}
	if got.SystemPrompt.String != "Our own prompt." || len(got.AllowedTools) != 0 || got.WorkspaceReadonly {
		t.Fatalf("operator settings changed: prompt %q, tools %v, read-only %v", got.SystemPrompt.String, got.AllowedTools, got.WorkspaceReadonly)
	}
}

// Each upgrade leaves a PROFILE_UPGRADED event on the system project, so an
// operator can see what changed on startup.
func TestSeedUpgradeRecordsProfileUpgradedEvent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "global.db")
	store, err := openTestStore(t, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{ID: "ops-bot", Name: "Ops", Role: "CODE_GEN"}); err != nil {
		t.Fatalf("UpsertAgentProfile: %v", err)
	}
	if err := seedDefaultAgent(ctx, store, false); err != nil {
		t.Fatalf("seedDefaultAgent: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	var payload string
	err = db.QueryRowContext(ctx,
		`SELECT payload FROM events WHERE type = ? AND project_id = ?`,
		string(models.EventTypeProfileUpgraded), models.SystemProjectID).Scan(&payload)
	if err != nil {
		t.Fatalf("no PROFILE_UPGRADED event on the system project: %v", err)
	}
	if payload == "" {
		t.Fatal("PROFILE_UPGRADED event has an empty payload")
	}
}

// The seeded researcher is agentic and read-only: it can explore the workspace
// but not change it.
func TestSeededResearcherIsReadOnlyAgenticProfile(t *testing.T) {
	ctx := context.Background()
	store, err := openTestStore(t, filepath.Join(t.TempDir(), "global.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := seedDefaultAgent(ctx, store, false); err != nil {
		t.Fatalf("seedDefaultAgent: %v", err)
	}
	got, err := store.GetAgentProfile(ctx, "researcher")
	if err != nil {
		t.Fatalf("GetAgentProfile(researcher): %v", err)
	}
	if !got.AgenticMode {
		t.Fatal("researcher is not agentic")
	}
	if !got.WorkspaceReadonly {
		t.Fatal("researcher workspace is writable; it must be read-only")
	}
	if len(got.AllowedTools) != 2 || got.AllowedTools[0] != "bash" || got.AllowedTools[1] != "read" {
		t.Fatalf("researcher allowed_tools = %v, want [bash read]", got.AllowedTools)
	}
}

// An install from before this change has a researcher row in legacy mode with
// the old prompt. The upgrade makes it agentic and read-only.
func TestSeedUpgradesLegacyResearcherToReadOnly(t *testing.T) {
	ctx := context.Background()
	store, err := openTestStore(t, filepath.Join(t.TempDir(), "global.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()
	legacy := models.AgentProfile{
		ID: "researcher", Name: "Research Specialist", Role: "RESEARCH", Temperature: 0.7, MaxTokens: 2048,
		SystemPrompt: sql.NullString{String: legacyResearcherSystemPrompt, Valid: true},
	}
	if err := store.UpsertAgentProfile(ctx, legacy); err != nil {
		t.Fatalf("UpsertAgentProfile: %v", err)
	}
	if err := seedDefaultAgent(ctx, store, false); err != nil {
		t.Fatalf("seedDefaultAgent: %v", err)
	}
	got, err := store.GetAgentProfile(ctx, "researcher")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if !got.AgenticMode || !got.WorkspaceReadonly || len(got.AllowedTools) != 2 {
		t.Fatalf("upgraded researcher: agentic %v, read-only %v, tools %v", got.AgenticMode, got.WorkspaceReadonly, got.AllowedTools)
	}
	if got.SystemPrompt.String == legacyResearcherSystemPrompt {
		t.Fatal("untouched legacy researcher kept its JSON prompt")
	}
}
