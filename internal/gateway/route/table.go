// Package route decides which agent handles a model call. It maps a Purpose and
// the task's attributes to an agent, and validates the agents: and stages:
// config sections. The gateway calls Resolve for every request; a table with no
// stage routes leaves every call on the gateway cascade, as it was before.
package route

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"agentd/internal/gateway/spec"
)

// DefaultAgentID is the seeded profile every task gets when it names none. It
// is not a pin: a task on it still follows its stage's rules. It matches the
// kanban store's default agent ID.
const DefaultAgentID = "default"

// Decision rules. A matching rule is named by its condition instead, for example
// requires_workspace_change=true (see Match.name).
const (
	RulePin     = "pin"
	RuleDefault = "default"
	RuleCascade = "cascade"
)

// Agent kinds. The kind fixes the level: a model agent is one gateway call
// (L1); an acp agent is an external agent driven over ACP (L3), which replaces
// the whole stage agent above the gateway.
const (
	KindModel = "model"
	KindACP   = "acp"
)

// Agent is a routing target: the provider and model a rule or a stage default
// sends a call to, or the preset of an acp agent, and the level it provides.
type Agent struct {
	Kind     string
	Provider string
	Model    string
	Preset   string
	Level    spec.Level
	// Executable and Credentials override an acp preset's binary on PATH and
	// the user's login file it copies from.
	Executable  string
	Credentials string
}

// Match is a rule's condition. Every condition that is set must hold. A nil
// field is not checked; a rule needs at least one set field to be valid.
type Match struct {
	RequiresWorkspaceChange *bool
}

// Rule sends a call to agent Use when Match holds. Rules are tried in order.
// MinLevel, when set, raises the level a matching call needs (T-043: a task
// that changes code needs L3); it can never lower the purpose's minimum.
type Rule struct {
	Match    Match
	Use      string
	MinLevel spec.Level
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
	for _, r := range sr.Rules {
		if r.Match.matches(attrs) {
			if levelRank[r.MinLevel] > levelRank[d.Required] {
				d.Required = r.MinLevel
			}
			return d.choose(t, r.Use, r.Match.name())
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

// name labels a rule by its condition, as the config's when: writes it. The
// first matching rule of a stage wins, so no two rules that can fire share a
// condition: unlike an index, the name stays the rule's when rules are
// inserted or reordered.
func (m Match) name() string {
	if m.RequiresWorkspaceChange == nil {
		return "always" // only an unvalidated table has a rule with no condition
	}
	return "requires_workspace_change=" + strconv.FormatBool(*m.RequiresWorkspaceChange)
}

// levelRank orders the levels an agent can provide. Only L1 and L3 are agent
// levels; the others are purpose-only markers.
var levelRank = map[spec.Level]int{spec.LevelL1: 1, spec.LevelL3: 3}

// stageAgentPurposes are the purposes whose stage agent the worker picks per
// call (Worker.taskCreationAgent, resolveExecutionAgent), the only ones an acp
// agent can run. Every other purpose is a gateway call, which only a model
// agent can serve.
var stageAgentPurposes = map[spec.Purpose]bool{
	spec.PurposeCreateElicit:    true,
	spec.PurposeCreateSplitTask: true,
	spec.PurposeCreateNextPhase: true,
	spec.PurposeExecWorkPlan:    true,
	spec.PurposeExecTurn:        true,
}

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
	check := func(where, name string, need spec.Level, minimum string) {
		a, ok := t.Agents[name]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("stages.%s.%s: unknown agent %q", purpose, where, name))
		case levelRank[a.Level] < levelRank[need]:
			errs = append(errs, fmt.Errorf("stages.%s.%s: agent %q is level %s, below the %s minimum %s", purpose, where, name, a.Level, minimum, need))
		case a.Kind == KindACP && !stageAgentPurposes[purpose]:
			errs = append(errs, fmt.Errorf("stages.%s.%s: agent %q is an acp agent, and %s is a gateway call only a model agent can serve", purpose, where, name, purpose))
		}
	}
	if sr.Default != "" {
		check("default", sr.Default, minLevel, "purpose")
	}
	for i, r := range sr.Rules {
		where := fmt.Sprintf("rules[%d]", i)
		if r.Match.RequiresWorkspaceChange == nil {
			errs = append(errs, fmt.Errorf("stages.%s.%s: rule has no condition", purpose, where))
		}
		need, minimum, err := ruleLevel(minLevel, r.MinLevel)
		if err != nil {
			errs = append(errs, fmt.Errorf("stages.%s.%s: %w", purpose, where, err))
		}
		if r.Use == "" {
			errs = append(errs, fmt.Errorf("stages.%s.%s: rule has no use", purpose, where))
			continue
		}
		check(where, r.Use, need, minimum)
	}
	return errs
}

// ruleLevel is the level a rule's use agent must provide, and which minimum it
// is: the rule's min_level when it raises the purpose's, else the purpose's.
func ruleLevel(purposeMin, ruleMin spec.Level) (spec.Level, string, error) {
	switch {
	case ruleMin == "":
		return purposeMin, "purpose", nil
	case levelRank[ruleMin] == 0:
		return purposeMin, "purpose", fmt.Errorf("min_level %q is not an agent level (want L1 or L3)", ruleMin)
	case levelRank[ruleMin] < levelRank[purposeMin]:
		return purposeMin, "purpose", fmt.Errorf("min_level %s would lower the purpose minimum %s", ruleMin, purposeMin)
	}
	return ruleMin, "rule", nil
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
