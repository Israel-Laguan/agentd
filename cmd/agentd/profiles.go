package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"agentd/internal/config"
	"agentd/internal/kanban"
	"agentd/internal/models"
)

// initProfileHint returns operator-facing lines about seeded profiles and detected providers.
func initProfileHint(gw config.GatewayConfig) string {
	check := config.CheckProvidersOffline(gw)
	hint := "profiles: default, researcher, qa, tier-context, tier-decision, tier-execute, tier-verify, tier-escalate (empty provider/model → gateway.order cascade)\n"
	if check.Available && check.Provider != "" {
		hint += fmt.Sprintf("detected LLM provider: %s\n", check.Provider)
	} else {
		hint += "no LLM API keys detected yet; configure OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY, or gateway.providers before agentd start\n"
	}
	hint += "after agentd start: PATCH /api/v1/agents/<id> with {\"provider\":\"...\",\"model\":\"...\"} to pin routing (repeat init preserves existing profiles)\n"
	if gatewayOrderIncludes(gw.Order, "gemini") || check.Provider == "gemini" {
		hint += "gemini free tier: agentd start --skip-llm-warmup to skip the billable warmup probe\n"
	}
	hint += "re-run with --reset-profiles to overwrite existing profile provider/model values\n"
	return hint
}

func gatewayOrderIncludes(order []string, name string) bool {
	for _, p := range order {
		if p == name {
			return true
		}
	}
	return false
}

// seedDefaultAgent installs default agent profiles into the store.
// On a fresh database all three profiles are written. On subsequent
// calls (e.g. repeated "agentd init") each profile is skipped when it
// already exists, preserving any operator PATCH. Pass reset=true to
// force-overwrite all profiles regardless of existing data. Either way, every
// profile still in non-agentic mode is upgraded afterwards.
func seedDefaultAgent(ctx context.Context, store *kanban.Store, reset bool) error {
	profiles := append(defaultAgentProfiles(), tieredAgentProfiles()...)
	for _, profile := range profiles {
		if !reset {
			existing, err := store.GetAgentProfile(ctx, profile.ID)
			if err == nil && existing != nil {
				continue
			}
			if err != nil && !errors.Is(err, models.ErrAgentProfileNotFound) {
				return err
			}
		}
		if err := store.UpsertAgentProfile(ctx, profile); err != nil {
			return err
		}
	}
	return upgradeNonAgenticProfiles(ctx, store, profiles)
}

// legacySeededPrompts are the prompts the seeded profiles carried while they
// were not agentic. A row still carrying one is untouched by the operator.
var legacySeededPrompts = map[string]string{
	"default":    legacyDefaultSystemPrompt,
	"researcher": legacyResearcherSystemPrompt,
	"qa":         legacyQASystemPrompt,
}

// legacyDefaultSystemPrompt is the default profile's prompt before B-080: the
// legacy one-command path. An untouched row still carrying it is upgraded.
const legacyDefaultSystemPrompt = "Suggest one safe shell command for the requested task. Output only JSON."

// legacyResearcherSystemPrompt and legacyQASystemPrompt are the seeded prompts
// before those profiles were agentic.
const (
	legacyResearcherSystemPrompt = "Investigate and summarize. Prefer information-gathering shell commands; never mutate state. Output only JSON."
	legacyQASystemPrompt         = "Run tests, lints, and assertions. Be deterministic and conservative. Output only JSON."
)

// defaultSystemPrompt drives the agentic loop: the work is done with tools, and
// a task is only reported Completed after its own tests pass (B-080).
const defaultSystemPrompt = "Do the requested task in the workspace with the tools you have. Run the project's tests before you report Completed. If a key detail is missing, or a check cannot run, report NeedsHuman with one question instead of guessing."

// researcherSystemPrompt names the researcher's job. Its read-only part is held
// by the profile's allowed tools and read-only workspace, not by this text.
const researcherSystemPrompt = "Investigate and summarize. Use bash and read only; never modify the workspace. Report what you find, including where you looked."

// qaSystemPrompt drives the agentic loop for QA runs.
const qaSystemPrompt = "Run tests, lints, and assertions. Be deterministic and conservative."

// upgradeNonAgenticProfiles upgrades every profile row still in non-agentic
// mode, operator-created rows included (see upgradeLegacyProfile).
func upgradeNonAgenticProfiles(ctx context.Context, store *kanban.Store, seeds []models.AgentProfile) error {
	rows, err := store.ListAgentProfiles(ctx)
	if err != nil {
		return err
	}
	seeded := make(map[string]*models.AgentProfile, len(seeds))
	for i := range seeds {
		seeded[seeds[i].ID] = &seeds[i]
	}
	for i := range rows {
		if rows[i].AgenticMode {
			continue
		}
		if err := upgradeLegacyProfile(ctx, store, &rows[i], seeded[rows[i].ID]); err != nil {
			return err
		}
	}
	return nil
}

// upgradeLegacyProfile makes a non-agentic row agentic. A seeded row also takes
// the seeded read-only policy when the operator set no tools, and the seeded
// prompt while it still carries the legacy one. Everything else the operator set
// is kept, and the upgrade is logged and recorded as an event.
func upgradeLegacyProfile(ctx context.Context, store *kanban.Store, existing *models.AgentProfile, seeded *models.AgentProfile) error {
	existing.AgenticMode = true
	if seeded != nil {
		if legacy, ok := legacySeededPrompts[existing.ID]; ok && existing.SystemPrompt.Valid && existing.SystemPrompt.String == legacy {
			existing.SystemPrompt = seeded.SystemPrompt
		}
		if len(existing.AllowedTools) == 0 && !existing.WorkspaceReadonly {
			existing.AllowedTools = seeded.AllowedTools
			existing.WorkspaceReadonly = seeded.WorkspaceReadonly
		}
	}
	if err := store.UpsertAgentProfile(ctx, *existing); err != nil {
		return err
	}
	slog.Info("profile upgraded to agentic", "profile_id", existing.ID,
		"allowed_tools", existing.AllowedTools, "workspace_readonly", existing.WorkspaceReadonly)
	return recordProfileUpgrade(ctx, store, existing)
}

// recordProfileUpgrade leaves a PROFILE_UPGRADED event on the system project,
// where operators look for startup changes.
func recordProfileUpgrade(ctx context.Context, store *kanban.Store, profile *models.AgentProfile) error {
	system, err := store.EnsureSystemProject(ctx)
	if err != nil {
		return err
	}
	return store.AppendEvent(ctx, models.Event{
		ProjectID: system.ID,
		Type:      models.EventTypeProfileUpgraded,
		Payload: fmt.Sprintf("profile %s is now agentic (allowed_tools=%v, workspace_readonly=%t)",
			profile.ID, profile.AllowedTools, profile.WorkspaceReadonly),
	})
}

func defaultAgentProfiles() []models.AgentProfile {
	return []models.AgentProfile{
		{
			ID: "default", Name: "Default Coding Agent",
			Provider: "", Model: "", Temperature: 0.2, MaxTokens: 1024,
			Role: "CODE_GEN", AgenticMode: true,
			SystemPrompt: sql.NullString{
				String: defaultSystemPrompt,
				Valid:  true,
			},
		},
		{
			ID: "researcher", Name: "Research Specialist",
			Provider: "", Model: "", Temperature: 0.7, MaxTokens: 2048,
			Role: "RESEARCH", AgenticMode: true,
			// Read-only: bash and read, with the workspace mounted read-only (SP-028).
			AllowedTools:      []string{"bash", "read"},
			WorkspaceReadonly: true,
			SystemPrompt: sql.NullString{
				String: researcherSystemPrompt,
				Valid:  true,
			},
		},
		{
			ID: "qa", Name: "Quality Assurance",
			Provider: "", Model: "", Temperature: 0.0, MaxTokens: 1024,
			Role: "QA", AgenticMode: true,
			SystemPrompt: sql.NullString{
				String: qaSystemPrompt,
				Valid:  true,
			},
		},
	}
}

// tieredAgentProfiles are the per-step profiles the tiered execution pipeline
// stamps onto its DAG children (see internal/queue/worker/splitter.go). A
// tiered step whose profile is missing fails dispatch with
// ErrAgentProfileNotFound, so all five ship with the defaults.
//
// Provider/model are left empty on purpose: profile lookup falls back to the
// gateway.role_models cascade. Note that today every agentic call (all five
// tiered steps included) is dispatched with the same gateway.RoleWorker
// route (internal/queue/worker/agentic/handlers.go) — there is no per-step
// routing that automatically assigns a small/mid/strong model per tier.
// Seeding these profiles here only guarantees the steps resolve; an operator
// who wants the documented cheap/mid/strong cost tiers enforced at runtime
// must PATCH distinct Provider/Model values onto each tier-* profile ID
// directly (see initProfileHint). This is cost-modeling scaffolding, not an
// enforced routing guarantee.
func tieredAgentProfiles() []models.AgentProfile {
	return []models.AgentProfile{
		{
			ID: "tier-context", Name: "Tiered: Context Gatherer",
			Provider: "", Model: "", Temperature: 0.0, MaxTokens: 4096,
			Role: "RESEARCH", AgenticMode: true,
			SystemPrompt: sql.NullString{
				String: "Gather only the context the task needs. Read-only; never modify the workspace.",
				Valid:  true,
			},
		},
		{
			ID: "tier-decision", Name: "Tiered: Decision Maker",
			Provider: "", Model: "", Temperature: 0.0, MaxTokens: 2048,
			Role: "CODE_GEN", AgenticMode: true,
			SystemPrompt: sql.NullString{
				String: "Decide the approach from the sealed ContextPack. Plan only; never modify the workspace.",
				Valid:  true,
			},
		},
		{
			ID: "tier-execute", Name: "Tiered: Executor",
			Provider: "", Model: "", Temperature: 0.0, MaxTokens: 4096,
			Role: "CODE_GEN", AgenticMode: true,
			SystemPrompt: sql.NullString{
				String: "Apply the decided changes within the decision's touch list.",
				Valid:  true,
			},
		},
		{
			ID: "tier-verify", Name: "Tiered: Verifier",
			Provider: "", Model: "", Temperature: 0.0, MaxTokens: 2048,
			Role: "QA", AgenticMode: true,
			SystemPrompt: sql.NullString{
				String: "Run the decided checks and classify the outcome. Do not invent new tests.",
				Valid:  true,
			},
		},
		{
			ID: "tier-escalate", Name: "Tiered: Escalation",
			Provider: "", Model: "", Temperature: 0.0, MaxTokens: 8192,
			Role: "CODE_GEN", AgenticMode: true,
			SystemPrompt: sql.NullString{
				String: "Resolve what execute and verify could not. Re-plan, then apply the smallest corrective change.",
				Valid:  true,
			},
		},
	}
}
