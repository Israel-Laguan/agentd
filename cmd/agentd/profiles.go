package main

import (
	"context"
	"database/sql"

	"agentd/internal/kanban"
	"agentd/internal/models"
)

// seedDefaultAgent installs (or re-installs) the protected "default"
// profile plus a small bench of specialist worker profiles so a fresh
// agentd database is ready to demonstrate per-agent provider/model
// routing. The "default" id is referenced in many places and cannot be
// deleted; the others are upserted only if absent so operator edits via
// PATCH /api/v1/agents/{id} are preserved across restarts.
func seedDefaultAgent(ctx context.Context, store *kanban.Store) error {
	for _, profile := range defaultAgentProfiles() {
		if profile.ID != "default" {
			if existing, err := store.GetAgentProfile(ctx, profile.ID); err == nil && existing != nil {
				continue
			}
		}
		if err := store.UpsertAgentProfile(ctx, profile); err != nil {
			return err
		}
	}
	return nil
}

func defaultAgentProfiles() []models.AgentProfile {
	return []models.AgentProfile{
		{
			ID: "default", Name: "Default Coding Agent",
			Provider: "openai", Model: "gpt-4o-mini", Temperature: 0.2, MaxTokens: 1024,
			Role: "CODE_GEN",
			SystemPrompt: sql.NullString{
				String: "Suggest one safe shell command for the requested task. Output only JSON.",
				Valid:  true,
			},
		},
		{
			ID: "researcher", Name: "Research Specialist",
			Provider: "openai", Model: "gpt-4o", Temperature: 0.7, MaxTokens: 2048,
			Role: "RESEARCH",
			SystemPrompt: sql.NullString{
				String: "Investigate and summarize. Prefer information-gathering shell commands; never mutate state. Output only JSON.",
				Valid:  true,
			},
		},
		{
			ID: "qa", Name: "Quality Assurance",
			Provider: "anthropic", Model: "claude-3-haiku", Temperature: 0.0, MaxTokens: 1024,
			Role: "QA",
			SystemPrompt: sql.NullString{
				String: "Run tests, lints, and assertions. Be deterministic and conservative. Output only JSON.",
				Valid:  true,
			},
		},
	}
}
