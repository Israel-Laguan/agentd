// Package route decides which agent handles a model call. It maps a Purpose and
// the task's attributes to an agent, and validates the agents: and stages:
// config sections. The gateway calls Resolve for every request; a table with no
// stage routes leaves every call on the gateway cascade, as it was before.
package route

import (
	"errors"
	"fmt"
	"sort"

	"agentd/internal/gateway/spec"
)

// DefaultAgentID is the seeded profile every task gets when it names none. It
// is not a pin: a task on it still follows its stage's rules. It matches the
// kanban store's default agent ID.
const DefaultAgentID = "default"

// Decision rules. A rule's name is "rules[i]" for the i-th rule of its stage.
const (
	RulePin     = "pin"
	RuleDefault = "default"
	RuleCascade = "cascade"
)

// Agent is a routing target: the provider and model a rule or a stage default
// sends a call to, and the level the agent provides.
type Agent struct {
	Provider string
	Model    string
	Level    spec.Level
}

// Match is a rule's condition. Every condition that is set must hold. A nil
// field is not checked; a rule needs at least one set field to be valid.
type Match struct {
	RequiresWorkspaceChange *bool
}

// Rule sends a call to agent Use when Match holds. Rules are tried in order.
type Rule struct {
	Match Match
	Use   string
}

// StageRoute is the route for one purpose: the agent it uses by default and the
// rules that override the default for particular tasks.
type StageRoute struct {
	Default string
	Rules   []Rule
}

// Table is the validated agents and stages config. Stages is keyed by Purpose.
type Table struct {
	Agents map[string]Agent
	Stages map[spec.Purpose]StageRoute
}

// Decision is one routing decision: the purpose asked for, the level it needs,
// the rule that matched, and the agent chosen. An empty Agent means the call
// keeps the gateway cascade.
type Decision struct {
	Purpose  spec.Purpose
	Required spec.Level
	Rule     string
	Agent    string
	Provider string
	Model    string
}

// IsPin reports whether an AgentID names an explicit profile rather than the
// seeded default or nothing.
func IsPin(agentID string) bool {
	return agentID != "" && agentID != DefaultAgentID
}

// Resolve picks the agent for one call. An explicit AgentID is a pin and wins
// over every rule. Otherwise the first matching rule wins, then the stage
// default, then the cascade.
func (t Table) Resolve(purpose spec.Purpose, agentID string, attrs spec.TaskAttrs) Decision {
	d := Decision{Purpose: purpose, Required: purpose.MinLevel(), Rule: RuleCascade}
	if IsPin(agentID) {
		d.Rule, d.Agent = RulePin, agentID
		return d
	}
	sr, ok := t.Stages[purpose]
	if !ok {
		return d
	}
	for i, r := range sr.Rules {
		if r.Match.matches(attrs) {
			return d.choose(t, r.Use, fmt.Sprintf("rules[%d]", i))
		}
	}
	if sr.Default != "" {
		return d.choose(t, sr.Default, RuleDefault)
	}
	return d
}

func (d Decision) choose(t Table, name, rule string) Decision {
	a := t.Agents[name]
	d.Rule, d.Agent, d.Provider, d.Model = rule, name, a.Provider, a.Model
	return d
}

func (m Match) matches(attrs spec.TaskAttrs) bool {
	if m.RequiresWorkspaceChange == nil {
		return true
	}
	if attrs.RequiresWorkspaceChange == nil {
		return false
	}
	return *m.RequiresWorkspaceChange == *attrs.RequiresWorkspaceChange
}

// levelRank orders the levels an agent can provide. Only L1 and L3 are agent
// levels; the others are purpose-only markers.
var levelRank = map[spec.Level]int{spec.LevelL1: 1, spec.LevelL3: 3}

// Validate checks the table at config load, so a bad table fails at startup and
// never at dispatch. It reports every problem it finds.
func (t Table) Validate() error {
	var errs []error
	for _, name := range sortedKeys(t.Agents) {
		if _, ok := levelRank[t.Agents[name].Level]; !ok {
			errs = append(errs, fmt.Errorf("agents.%s: level %q is not an agent level (want L1 or L3)", name, t.Agents[name].Level))
		}
	}
	for _, purpose := range sortedPurposes(t.Stages) {
		errs = append(errs, t.validateStage(purpose, t.Stages[purpose])...)
	}
	return errors.Join(errs...)
}

func (t Table) validateStage(purpose spec.Purpose, sr StageRoute) []error {
	minLevel := purpose.MinLevel()
	if _, ok := levelRank[minLevel]; !ok {
		return []error{fmt.Errorf("stages.%s: purpose has no routable level (%q)", purpose, minLevel)}
	}
	if sr.Default == "" && len(sr.Rules) == 0 {
		return []error{fmt.Errorf("stages.%s: no default and no rules", purpose)}
	}
	var errs []error
	check := func(where, name string) {
		a, ok := t.Agents[name]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("stages.%s.%s: unknown agent %q", purpose, where, name))
		case levelRank[a.Level] < levelRank[minLevel]:
			errs = append(errs, fmt.Errorf("stages.%s.%s: agent %q is level %s, below the purpose minimum %s", purpose, where, name, a.Level, minLevel))
		}
	}
	if sr.Default != "" {
		check("default", sr.Default)
	}
	for i, r := range sr.Rules {
		where := fmt.Sprintf("rules[%d]", i)
		if r.Match.RequiresWorkspaceChange == nil {
			errs = append(errs, fmt.Errorf("stages.%s.%s: rule has no condition", purpose, where))
		}
		if r.Use == "" {
			errs = append(errs, fmt.Errorf("stages.%s.%s: rule has no use", purpose, where))
			continue
		}
		check(where, r.Use)
	}
	return errs
}

func sortedKeys(m map[string]Agent) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedPurposes(m map[spec.Purpose]StageRoute) []spec.Purpose {
	keys := make([]spec.Purpose, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
