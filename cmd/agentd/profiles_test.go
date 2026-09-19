package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/queue/worker"
)

type initProfileHintCase struct {
	name    string
	gw      config.GatewayConfig
	want    []string
	notWant []string
}

var initProfileHintCases = []initProfileHintCase{
	{
		name: "no keys",
		gw:   config.GatewayConfig{Order: []string{"openai"}},
		want: []string{
			"gateway.order cascade",
			"no LLM API keys detected",
			"PATCH /api/v1/agents/<id>",
			"repeat init preserves",
			"--reset-profiles",
		},
		notWant: []string{"--skip-llm-warmup"},
	},
	{
		name: "gemini in order with key",
		gw: config.GatewayConfig{
			Order: []string{"gemini"},
			Gemini: gateway.ProviderConfig{
				APIKey: "test-key",
				Model:  "gemini-2.5-flash",
			},
		},
		want: []string{
			"detected LLM provider: gemini",
			"--skip-llm-warmup",
			"PATCH /api/v1/agents/<id>",
		},
	},
	{
		name: "openai key with gemini in order",
		gw: config.GatewayConfig{
			Order:  []string{"openai", "gemini"},
			OpenAI: gateway.ProviderConfig{APIKey: "sk-test"},
		},
		want: []string{
			"detected LLM provider: openai",
			"PATCH /api/v1/agents/<id>",
			"--skip-llm-warmup",
		},
	},
	{
		name: "openai only",
		gw: config.GatewayConfig{
			Order:  []string{"openai"},
			OpenAI: gateway.ProviderConfig{APIKey: "sk-test"},
		},
		want: []string{
			"detected LLM provider: openai",
		},
		notWant: []string{"--skip-llm-warmup"},
	},
}

func TestInitProfileHint(t *testing.T) {
	t.Parallel()
	for _, tc := range initProfileHintCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := initProfileHint(tc.gw)
			assertContainsAll(t, got, tc.want)
			assertContainsNone(t, got, tc.notWant)
		})
	}
}

func assertContainsAll(t *testing.T, got string, want []string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("initProfileHint missing %q\n%s", s, got)
		}
	}
}

func assertContainsNone(t *testing.T, got string, notWant []string) {
	t.Helper()
	for _, s := range notWant {
		if strings.Contains(got, s) {
			t.Errorf("initProfileHint should not contain %q\n%s", s, got)
		}
	}
}

// TestSeededProfilesCoverTieredSteps asserts every tiered step kind has a
// seeded agent profile that is actually persisted and retrievable after
// running seedDefaultAgent against a real store. A tiered step whose profile
// is missing fails dispatch with ErrAgentProfileNotFound, which would
// silently break a ladder rung — adding a step kind without seeding its
// profile fails here instead.
func TestSeededProfilesCoverTieredSteps(t *testing.T) {
	home := t.TempDir()
	store, err := openTestStore(t, filepath.Join(home, "global.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := seedDefaultAgent(context.Background(), store, false); err != nil {
		t.Fatalf("seedDefaultAgent: %v", err)
	}

	for kind, profileID := range worker.TieredStepProfiles() {
		if _, err := store.GetAgentProfile(context.Background(), profileID); err != nil {
			t.Errorf("tiered step %q needs profile %q, which seedDefaultAgent does not install: %v", kind, profileID, err)
		}
	}
}

// TestTieredProfilesLeaveRoutingToGateway asserts only that the seeded tier
// profiles ship with empty Provider/Model. It does NOT verify that distinct
// small/mid/strong models are actually applied per tier at runtime — today
// every agentic call, tiered steps included, is dispatched through the same
// gateway.RoleWorker route (see internal/queue/worker/agentic/handlers.go),
// so there is no automatic per-step model routing. An operator who wants the
// documented cost tiers enforced must PATCH Provider/Model onto each
// tier-* profile explicitly.
func TestTieredProfilesLeaveRoutingToGateway(t *testing.T) {
	t.Parallel()
	for _, p := range tieredAgentProfiles() {
		if p.Provider != "" || p.Model != "" {
			t.Errorf("profile %q pins provider/model (%q/%q); expected empty so gateway.role_models/manual PATCH decides routing",
				p.ID, p.Provider, p.Model)
		}
	}
}
