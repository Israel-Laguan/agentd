package config

import (
	"strings"
	"testing"

	"agentd/internal/agent/acp"
	"agentd/internal/gateway/spec"
)

// No real preset exists until PR 08; the tests register one for kind: acp.
func init() {
	acp.Presets["fake"] = acp.Preset{Argv: []string{"fake-acp-agent"}}
}

// The agents: and stages: sections are validated at load. Each bad config
// below must fail Load and name the problem.
func TestLoad_RoutingValidSectionsLoad(t *testing.T) {
	cfg, err := loadConfigBody(t, `
agents:
  quick: { provider: gemini, model: flash-lite }
  big: { provider: openai, model: gpt-4o }
stages:
  create.elicit:
    default: quick
  exec.work_plan:
    default: quick
    rules:
      - when: { requires_workspace_change: true }
        use: quick
`)
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if got := cfg.Gateway.Routing.Agents["quick"]; got.Provider != "gemini" || got.Model != "flash-lite" || got.Level != spec.LevelL1 {
		t.Fatalf("agent quick = %+v, want gemini/flash-lite at the default level L1", got)
	}
	if got := cfg.Gateway.Routing.Agents["big"].Level; got != spec.LevelL1 {
		t.Fatalf("agent big level = %q, want L1: every config agent is a model call", got)
	}
	if got := cfg.Gateway.Routing.Stages[spec.PurposeCreateElicit].Default; got != "quick" {
		t.Fatalf("create.elicit default = %q, want quick", got)
	}
	rules := cfg.Gateway.Routing.Stages[spec.PurposeExecWorkPlan].Rules
	if len(rules) != 1 || rules[0].Use != "quick" || rules[0].Match.RequiresWorkspaceChange == nil || !*rules[0].Match.RequiresWorkspaceChange {
		t.Fatalf("exec.work_plan rules = %+v, want one rule on requires_workspace_change", rules)
	}
}

func TestLoad_RoutingACPKindIsL3AndRaisesALevel(t *testing.T) {
	cfg, err := loadConfigBody(t, `
agents:
  quick: { kind: model, provider: gemini }
  coder: { kind: acp, preset: fake }
stages:
  exec.turn:
    default: coder
  create.split_task:
    default: quick
    rules:
      - when: { requires_workspace_change: true }
        min_level: L3
        use: coder
`)
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if got := cfg.Gateway.Routing.Agents["coder"]; got.Kind != "acp" || got.Preset != "fake" || got.Level != spec.LevelL3 {
		t.Fatalf("agent coder = %+v, want an acp agent on preset fake at L3", got)
	}
	if got := cfg.Gateway.Routing.Agents["quick"]; got.Kind != "model" || got.Level != spec.LevelL1 {
		t.Fatalf("agent quick = %+v, want a model agent at L1", got)
	}
	if rules := cfg.Gateway.Routing.Stages[spec.PurposeCreateSplitTask].Rules; len(rules) != 1 || rules[0].MinLevel != spec.LevelL3 {
		t.Fatalf("create.split_task rules = %+v, want one rule raising to L3", rules)
	}
}

func TestLoad_RoutingWithNoSectionsIsEmpty(t *testing.T) {
	cfg, err := loadConfigBody(t, "")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(cfg.Gateway.Routing.Stages) != 0 || len(cfg.Gateway.Routing.Agents) != 0 {
		t.Fatalf("Routing = %+v, want an empty table so every call keeps the cascade", cfg.Gateway.Routing)
	}
}

// routingLoadErrorCases are configs that Load must refuse, each with the text the
// error must name.
var routingLoadErrorCases = []struct {
	name string
	body string
	want string
}{
	{
		name: "unknown agent as a stage default",
		body: "agents:\n  quick: { provider: gemini }\nstages:\n  create.elicit:\n    default: ghost\n",
		want: `unknown agent "ghost"`,
	},
	{
		name: "unknown agent as a rule target",
		body: "agents:\n  quick: { provider: gemini }\nstages:\n  exec.work_plan:\n    default: quick\n    rules:\n      - when: { requires_workspace_change: true }\n        use: ghost\n",
		want: `unknown agent "ghost"`,
	},
	{
		name: "rule with an unknown condition",
		body: "agents:\n  quick: { provider: gemini }\nstages:\n  exec.work_plan:\n    default: quick\n    rules:\n      - when: { size: large }\n        use: quick\n",
		want: `unknown key "size"`,
	},
	{
		name: "rule with no condition",
		body: "agents:\n  quick: { provider: gemini }\nstages:\n  exec.work_plan:\n    default: quick\n    rules:\n      - when: {}\n        use: quick\n",
		want: "rule has no condition",
	},
	{
		name: "rule with a non-boolean condition",
		body: "agents:\n  quick: { provider: gemini }\nstages:\n  exec.work_plan:\n    default: quick\n    rules:\n      - when: { requires_workspace_change: yes-please }\n        use: quick\n",
		want: "requires_workspace_change must be true or false",
	},
	{
		name: "route to an agent below the purpose minimum level",
		body: "agents:\n  quick: { provider: gemini }\nstages:\n  exec.turn:\n    default: quick\n",
		want: "below the purpose minimum L3",
	},
	{
		name: "unknown purpose",
		body: "agents:\n  quick: { provider: gemini }\nstages:\n  exec.nope:\n    default: quick\n",
		want: "purpose has no routable level",
	},
	{
		name: "level was removed",
		body: "agents:\n  quick: { provider: gemini, level: L2 }\nstages:\n  create.elicit:\n    default: quick\n",
		want: "level was removed",
	},
	{
		name: "an agent cannot claim L3 by writing it",
		body: "agents:\n  big: { provider: openai, level: L3 }\nstages:\n  exec.turn:\n    default: big\n",
		want: "level was removed",
	},
	{
		name: "a model agent routed to exec.turn",
		body: "agents:\n  big: { provider: openai }\nstages:\n  exec.turn:\n    default: big\n",
		want: "below the purpose minimum L3",
	},
	{
		name: "kind model routed to exec.turn names the minimum",
		body: "agents:\n  big: { kind: model, provider: openai }\nstages:\n  exec.turn:\n    default: big\n",
		want: "below the purpose minimum L3",
	},
	{
		name: "unknown kind",
		body: "agents:\n  big: { kind: cli, provider: openai }\nstages:\n  create.elicit:\n    default: big\n",
		want: `unknown kind "cli"`,
	},
	{
		name: "acp agent with an unknown preset",
		body: "agents:\n  coder: { kind: acp, preset: opencode }\nstages:\n  exec.turn:\n    default: coder\n",
		want: `unknown preset "opencode"`,
	},
	{
		name: "acp agent names a provider",
		body: "agents:\n  coder: { kind: acp, preset: fake, provider: openai }\nstages:\n  exec.turn:\n    default: coder\n",
		want: `unknown key "provider"`,
	},
	{
		name: "acp agent on a gateway call",
		body: "agents:\n  coder: { kind: acp, preset: fake }\nstages:\n  maint.summarize_log:\n    default: coder\n",
		want: "only a model agent can serve",
	},
	{
		name: "rule that lowers the level",
		body: "agents:\n  coder: { kind: acp, preset: fake }\nstages:\n  exec.turn:\n    default: coder\n    rules:\n      - when: { requires_workspace_change: false }\n        min_level: L1\n        use: coder\n",
		want: "would lower the purpose minimum L3",
	},
	{
		name: "misspelt stage key",
		body: "agents:\n  quick: { provider: gemini }\nstages:\n  create.elicit:\n    defualt: quick\n",
		want: `unknown key "defualt"`,
	},
}

func TestLoad_RoutingErrors(t *testing.T) {
	for _, tc := range routingLoadErrorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadConfigBody(t, tc.body)
			if err == nil {
				t.Fatalf("Load() = nil, want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
