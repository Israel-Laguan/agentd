package kanban

import (
	"context"
	"slices"
	"testing"

	"agentd/internal/models"
)

// AllowedTools and WorkspaceReadonly must survive a round trip through the
// store, on both the single-profile and the list read paths.
func TestAgentProfileRoundTripsAllowedToolsAndWorkspaceReadonly(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	want := models.AgentProfile{
		ID: "researcher", Name: "Researcher", Provider: "openai", Model: "gpt-4o-mini",
		Role: "RESEARCH", AgenticMode: true,
		AllowedTools:      []string{"read", "bash"},
		WorkspaceReadonly: true,
	}
	if err := store.UpsertAgentProfile(ctx, want); err != nil {
		t.Fatalf("upsert researcher: %v", err)
	}

	got, err := store.GetAgentProfile(ctx, "researcher")
	if err != nil {
		t.Fatalf("get researcher: %v", err)
	}
	if !slices.Equal(got.AllowedTools, want.AllowedTools) {
		t.Fatalf("AllowedTools = %v, want %v", got.AllowedTools, want.AllowedTools)
	}
	if !got.WorkspaceReadonly {
		t.Fatal("WorkspaceReadonly = false, want true")
	}

	list, err := store.ListAgentProfiles(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range list {
		if p.ID != "researcher" {
			continue
		}
		if !slices.Equal(p.AllowedTools, want.AllowedTools) || !p.WorkspaceReadonly {
			t.Fatalf("listed researcher = %+v, want the stored allowlist and read-only flag", p)
		}
	}
}

// A profile with no allowlist stores and reads back as unrestricted.
func TestAgentProfileWithoutAllowlistReadsBackUnrestricted(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{
		ID: "plain", Name: "Plain", Provider: "openai", Model: "gpt-4o-mini", Role: "CODE_GEN",
	}); err != nil {
		t.Fatalf("upsert plain: %v", err)
	}
	got, err := store.GetAgentProfile(ctx, "plain")
	if err != nil {
		t.Fatalf("get plain: %v", err)
	}
	if len(got.AllowedTools) != 0 || got.WorkspaceReadonly {
		t.Fatalf("AllowedTools = %v, WorkspaceReadonly = %t; want no allowlist and writable", got.AllowedTools, got.WorkspaceReadonly)
	}
}
