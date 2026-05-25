package main

import (
	"context"
	"database/sql"
	"fmt"

	"agentd/internal/config"
	"agentd/internal/kanban"
	"agentd/internal/models"
)

// initProfileHint returns operator-facing lines about seeded profiles and detected providers.
func initProfileHint(gw config.GatewayConfig) string {
	check := config.CheckProviders(gw)
	hint := "profiles: default, researcher, qa (empty provider/model → gateway.order cascade)\n"
	if check.Available && check.Provider != "" {
		hint += fmt.Sprintf("detected LLM provider: %s\n", check.Provider)
	} else {
		hint += "no LLM API keys detected yet; configure OPENAI_API_KEY, GEMINI_API_KEY, or gateway.providers before agentd start\n"
	}
	hint += "re-run with --reset-profiles to overwrite existing profile provider/model values\n"
	return hint
}

// seedDefaultAgent installs default agent profiles into the store.
// On a fresh database all three profiles are written. On subsequent
// calls (e.g. repeated "agentd init") each profile is skipped when it
// already exists, preserving any operator PATCH. Pass reset=true to
// force-overwrite all profiles regardless of existing data.
func seedDefaultAgent(ctx context.Context, store *kanban.Store, reset bool) error {
	for _, profile := range defaultAgentProfiles() {
		if !reset {
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
			Provider: "", Model: "", Temperature: 0.2, MaxTokens: 1024,
			Role: "CODE_GEN",
			SystemPrompt: sql.NullString{
				String: "Suggest one safe shell command for the requested task. Output only JSON.",
				Valid:  true,
			},
		},
		{
			ID: "researcher", Name: "Research Specialist",
			Provider: "", Model: "", Temperature: 0.7, MaxTokens: 2048,
			Role: "RESEARCH",
			SystemPrompt: sql.NullString{
				String: "Investigate and summarize. Prefer information-gathering shell commands; never mutate state. Output only JSON.",
				Valid:  true,
			},
		},
		{
			ID: "qa", Name: "Quality Assurance",
			Provider: "", Model: "", Temperature: 0.0, MaxTokens: 1024,
			Role: "QA",
			SystemPrompt: sql.NullString{
				String: "Run tests, lints, and assertions. Be deterministic and conservative. Output only JSON.",
				Valid:  true,
			},
		},
	}
}
