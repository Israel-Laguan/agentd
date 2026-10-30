package config

import (
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

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
		name: "level is not a key",
		body: "agents:\n  quick: { provider: gemini, level: L2 }\nstages:\n  create.elicit:\n    default: quick\n",
		want: `unknown key "level"`,
	},
	{
		name: "an agent cannot claim L3 by writing it",
		body: "agents:\n  big: { provider: openai, level: L3 }\nstages:\n  exec.turn:\n    default: big\n",
		want: `unknown key "level"`,
	},
	{
		name: "a model agent routed to exec.turn",
		body: "agents:\n  big: { provider: openai }\nstages:\n  exec.turn:\n    default: big\n",
		want: "below the purpose minimum L3",
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
