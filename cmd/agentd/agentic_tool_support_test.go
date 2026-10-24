package main

import (
	"context"
	"slices"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// At startup the daemon names every agentic profile whose provider cannot call
// tools, so the operator sees why its tasks will fail. Non-agentic profiles are
// left alone.
func TestAgenticProfilesWithoutToolsNamesOnlyAgenticToolLessProfiles(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	for _, p := range []models.AgentProfile{
		{ID: "stuck", Name: "Stuck", Provider: "ollama", Model: "llama3", AgenticMode: true},
		{ID: "plain", Name: "Plain", Provider: "ollama", Model: "llama3"},
		{ID: "fine", Name: "Fine", Provider: "openai", Model: "gpt-4o", AgenticMode: true},
	} {
		if err := store.UpsertAgentProfile(ctx, p); err != nil {
			t.Fatalf("upsert %s: %v", p.ID, err)
		}
	}
	toolCapable := func(provider string) bool { return provider == "openai" }

	got, err := agenticProfilesWithoutTools(ctx, store, toolCapable)
	if err != nil {
		t.Fatalf("agenticProfilesWithoutTools() error = %v", err)
	}
	if want := []string{"stuck"}; !slices.Equal(got, want) {
		t.Fatalf("flagged profiles = %v, want %v", got, want)
	}
}
