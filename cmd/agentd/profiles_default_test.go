package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/models"
)

// B-080: the default profile was seeded with agentic_mode off, so a task sent
// to it ran one shell command that changed nothing and was marked COMPLETED.
func TestSeededDefaultProfileIsAgentic(t *testing.T) {
	store, err := openTestStore(t, filepath.Join(t.TempDir(), "global.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	if err := seedDefaultAgent(ctx, store, false); err != nil {
		t.Fatalf("seedDefaultAgent: %v", err)
	}
	got, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("GetAgentProfile(default): %v", err)
	}
	if !got.AgenticMode {
		t.Fatal("default profile has agentic_mode off, which the API refuses and startup upgrades")
	}
}

// An install from before this fix kept the old default row. The upgrade makes
// it agentic. An untouched row also takes the seeded prompt; a customized prompt
// is kept as it is.
func TestSeedUpgradesLegacyDefaultAndKeepsOperatorPrompt(t *testing.T) {
	ctx := context.Background()
	untouched := models.AgentProfile{
		ID: "default", Name: "Default Coding Agent", Role: "CODE_GEN", Temperature: 0.2, MaxTokens: 1024,
		SystemPrompt: sql.NullString{String: legacyDefaultSystemPrompt, Valid: true},
	}
	customized := untouched
	customized.SystemPrompt = sql.NullString{String: "Our own prompt.", Valid: true}

	for _, tc := range []struct {
		name     string
		existing models.AgentProfile
		want     bool
	}{
		{"untouched legacy row is upgraded", untouched, true},
		{"operator prompt is kept as it is", customized, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := openTestStore(t, filepath.Join(t.TempDir(), "global.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer func() { _ = store.Close() }()
			if err := store.UpsertAgentProfile(ctx, tc.existing); err != nil {
				t.Fatalf("UpsertAgentProfile: %v", err)
			}
			if err := seedDefaultAgent(ctx, store, false); err != nil {
				t.Fatalf("seedDefaultAgent: %v", err)
			}
			got, err := store.GetAgentProfile(ctx, "default")
			if err != nil {
				t.Fatalf("GetAgentProfile: %v", err)
			}
			if got.AgenticMode != tc.want {
				t.Fatalf("AgenticMode = %v, want %v", got.AgenticMode, tc.want)
			}
			if tc.existing.SystemPrompt.String == "Our own prompt." && got.SystemPrompt.String != tc.existing.SystemPrompt.String {
				t.Fatalf("operator prompt was changed to %q", got.SystemPrompt.String)
			}
		})
	}
}

func TestDefaultProfilePromptAsksInsteadOfGuessing(t *testing.T) {
	for _, p := range defaultAgentProfiles() {
		if p.ID != "default" {
			continue
		}
		if !strings.Contains(p.SystemPrompt.String, "NeedsHuman") || !strings.Contains(p.SystemPrompt.String, "test") {
			t.Fatalf("default prompt = %q; want it to verify with tests and report NeedsHuman instead of guessing", p.SystemPrompt.String)
		}
		return
	}
	t.Fatal("no default profile seeded")
}
