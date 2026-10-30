package route

import (
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func boolPtr(b bool) *bool { return &b }

func workspaceRule(use string, want bool) Rule {
	return Rule{Match: Match{RequiresWorkspaceChange: boolPtr(want)}, Use: use}
}

func testTable() Table {
	return Table{
		Agents: map[string]Agent{
			"quick": {Provider: "gemini", Model: "flash", Level: spec.LevelL1},
			"big":   {Provider: "openai", Model: "gpt-4o", Level: spec.LevelL1},
		},
		Stages: map[spec.Purpose]StageRoute{
			spec.PurposeCreateElicit: {Default: "quick"},
			spec.PurposeExecWorkPlan: {
				Default: "big",
				Rules: []Rule{
					workspaceRule("quick", false),
					workspaceRule("big", true),
				},
			},
		},
	}
}

// Red tests first: the rules are tried in order, the first match wins, and
// a purpose with no route resolves to the gateway cascade.
func TestResolve_RuleOrderFirstMatchWins(t *testing.T) {
	table := Table{
		Agents: map[string]Agent{
			"first":  {Provider: "a", Model: "m1", Level: spec.LevelL1},
			"second": {Provider: "b", Model: "m2", Level: spec.LevelL1},
		},
		Stages: map[spec.Purpose]StageRoute{
			spec.PurposeCreateElicit: {
				Default: "second",
				Rules: []Rule{
					{Match: Match{RequiresWorkspaceChange: boolPtr(true)}, Use: "first"},
					{Match: Match{RequiresWorkspaceChange: boolPtr(true)}, Use: "second"},
				},
			},
		},
	}
	d := table.Resolve(spec.PurposeCreateElicit, "", spec.TaskAttrs{RequiresWorkspaceChange: boolPtr(true)})
	if d.Agent != "first" || d.Rule != "rules[0]" {
		t.Fatalf("Resolve() = agent %q rule %q, want agent first rule rules[0]", d.Agent, d.Rule)
	}
	if d.Provider != "a" || d.Model != "m1" {
		t.Fatalf("Resolve() provider/model = %q/%q, want a/m1", d.Provider, d.Model)
	}
}

func TestResolve_RuleSkippedWhenConditionFails(t *testing.T) {
	d := testTable().Resolve(spec.PurposeExecWorkPlan, "", spec.TaskAttrs{})
	if d.Agent != "big" || d.Rule != RuleDefault {
		t.Fatalf("Resolve() = agent %q rule %q, want big default", d.Agent, d.Rule)
	}
	d = testTable().Resolve(spec.PurposeExecWorkPlan, "", spec.TaskAttrs{RequiresWorkspaceChange: boolPtr(true)})
	if d.Agent != "big" || d.Rule != "rules[1]" {
		t.Fatalf("Resolve() = agent %q rule %q, want big rules[1]", d.Agent, d.Rule)
	}
	d = testTable().Resolve(spec.PurposeExecWorkPlan, "", spec.TaskAttrs{RequiresWorkspaceChange: boolPtr(false)})
	if d.Agent != "quick" || d.Rule != "rules[0]" {
		t.Fatalf("Resolve() = agent %q rule %q, want quick rules[0]", d.Agent, d.Rule)
	}
}

func TestResolve_FallsBackToStageDefault(t *testing.T) {
	d := testTable().Resolve(spec.PurposeCreateElicit, "", spec.TaskAttrs{RequiresWorkspaceChange: boolPtr(true)})
	if d.Agent != "quick" || d.Rule != RuleDefault {
		t.Fatalf("Resolve() = agent %q rule %q, want quick default", d.Agent, d.Rule)
	}
	if d.Required != spec.LevelL1 {
		t.Fatalf("Resolve() required = %q, want L1", d.Required)
	}
}

func TestResolve_NoRouteIsCascade(t *testing.T) {
	d := testTable().Resolve(spec.PurposeSystemWarmup, "", spec.TaskAttrs{})
	if d.Agent != "" || d.Rule != RuleCascade || d.Provider != "" || d.Model != "" {
		t.Fatalf("Resolve() = %+v, want an empty cascade decision", d)
	}
}

func TestResolve_EmptyTableLeavesEveryPurposeOnCascade(t *testing.T) {
	var table Table
	for _, p := range spec.AllPurposes() {
		if d := table.Resolve(p, "", spec.TaskAttrs{}); d.Rule != RuleCascade || d.Agent != "" {
			t.Fatalf("Resolve(%s) on empty table = %+v, want cascade", p, d)
		}
	}
}

func TestResolve_ExplicitAgentIDIsAPin(t *testing.T) {
	d := testTable().Resolve(spec.PurposeExecWorkPlan, "tier-context", spec.TaskAttrs{RequiresWorkspaceChange: boolPtr(true)})
	if d.Agent != "tier-context" || d.Rule != RulePin {
		t.Fatalf("Resolve() = agent %q rule %q, want tier-context pin", d.Agent, d.Rule)
	}
	if d.Provider != "" || d.Model != "" {
		t.Fatalf("a pin must not inject provider/model, got %q/%q", d.Provider, d.Model)
	}
}

func TestResolve_SeededDefaultProfileIsNotAPin(t *testing.T) {
	d := testTable().Resolve(spec.PurposeCreateElicit, DefaultAgentID, spec.TaskAttrs{})
	if d.Rule != RuleDefault || d.Agent != "quick" {
		t.Fatalf("Resolve() with the seeded default profile = agent %q rule %q, want stage default", d.Agent, d.Rule)
	}
}

func TestValidate_AcceptsWellFormedTable(t *testing.T) {
	if err := testTable().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidate_UnknownAgent(t *testing.T) {
	table := testTable()
	table.Stages[spec.PurposeCreateElicit] = StageRoute{Default: "nope"}
	assertInvalid(t, table, `unknown agent "nope"`)
}

func TestValidate_UnknownAgentInRule(t *testing.T) {
	table := testTable()
	table.Stages[spec.PurposeExecWorkPlan] = StageRoute{Default: "big", Rules: []Rule{workspaceRule("ghost", true)}}
	assertInvalid(t, table, `unknown agent "ghost"`)
}

func TestValidate_BadRules(t *testing.T) {
	cases := map[string]Rule{
		"no condition": {Use: "quick"},
		"no use":       {Match: Match{RequiresWorkspaceChange: boolPtr(true)}},
	}
	for name, rule := range cases {
		t.Run(name, func(t *testing.T) {
			table := testTable()
			table.Stages[spec.PurposeExecWorkPlan] = StageRoute{Default: "big", Rules: []Rule{rule}}
			assertInvalid(t, table, "rules[0]")
		})
	}
}

func TestValidate_RouteBelowPurposeMinimumLevel(t *testing.T) {
	table := testTable()
	table.Agents["quick"] = Agent{Provider: "gemini", Level: spec.LevelL1}
	// exec.turn needs L3; an L1 agent is below it.
	table.Stages[spec.PurposeExecTurn] = StageRoute{Default: "quick"}
	assertInvalid(t, table, "below the purpose minimum L3")
}

func TestValidate_RouteToPurposeWithoutRoutableLevel(t *testing.T) {
	table := testTable()
	table.Stages[spec.PurposeSystemWarmup] = StageRoute{Default: "quick"}
	assertInvalid(t, table, "has no routable level")
}

func TestValidate_UnknownAgentLevel(t *testing.T) {
	table := testTable()
	table.Agents["quick"] = Agent{Provider: "gemini", Level: "L2"}
	assertInvalid(t, table, `level "L2"`)
}

func TestValidate_StageWithNeitherDefaultNorRules(t *testing.T) {
	table := testTable()
	table.Stages[spec.PurposeCreateElicit] = StageRoute{}
	assertInvalid(t, table, "no default and no rules")
}

func assertInvalid(t *testing.T, table Table, want string) {
	t.Helper()
	err := table.Validate()
	if err == nil {
		t.Fatalf("Validate() = nil, want an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Validate() = %v, want it to contain %q", err, want)
	}
}
