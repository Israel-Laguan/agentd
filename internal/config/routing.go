package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
)

// defaultAgentLevel is the level an agent provides when its entry names none:
// a single model call.
const defaultAgentLevel = spec.LevelL1

// loadRouteTable reads and validates the agents: and stages: sections. A config
// with neither section yields an empty table, so every call keeps the cascade.
func loadRouteTable(v *viper.Viper) (route.Table, error) {
	agents, err := parseAgents(v.Get("agents"))
	if err != nil {
		return route.Table{}, err
	}
	stages, err := parseStages(v.Get("stages"))
	if err != nil {
		return route.Table{}, err
	}
	table := route.Table{Agents: agents, Stages: stages}
	if err := table.Validate(); err != nil {
		return route.Table{}, fmt.Errorf("routing config: %w", err)
	}
	return table, nil
}

func parseAgents(raw any) (map[string]route.Agent, error) {
	out := make(map[string]route.Agent)
	if raw == nil {
		return out, nil
	}
	entries, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("agents must be a map of agent name to settings")
	}
	for name, value := range entries {
		entry, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("agents.%s must be a map", name)
		}
		if err := onlyKeys(entry, "agents."+name, "provider", "model", "level"); err != nil {
			return nil, err
		}
		provider := ""
		if raw, ok := entry["provider"]; ok {
			text, isString := raw.(string)
			if !isString {
				return nil, fmt.Errorf("agents.%s: provider must be a string", name)
			}
			provider = strings.TrimSpace(text)
		}
		if provider == "" {
			return nil, fmt.Errorf("agents.%s: provider is required", name)
		}
		model := ""
		if raw, ok := entry["model"]; ok {
			text, isString := raw.(string)
			if !isString {
				return nil, fmt.Errorf("agents.%s: model must be a string", name)
			}
			model = strings.TrimSpace(text)
		}
		level := defaultAgentLevel
		if raw, ok := entry["level"]; ok {
			text, isString := raw.(string)
			if !isString {
				return nil, fmt.Errorf("agents.%s: level must be a string", name)
			}
			if parsed := spec.Level(strings.TrimSpace(text)); parsed != "" {
				level = parsed
			}
		}
		out[name] = route.Agent{Provider: provider, Model: model, Level: level}
	}
	return out, nil
}

// parseStages flattens the stage map first. Viper can keep a dotted purpose such
// as create.elicit as one key or split it into nested maps, so both are read.
func parseStages(raw any) (map[spec.Purpose]route.StageRoute, error) {
	out := make(map[spec.Purpose]route.StageRoute)
	if raw == nil {
		return out, nil
	}
	top, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("stages must be a map of purpose to route")
	}
	leaves := make(map[string]map[string]any)
	if err := collectStageLeaves(top, "", leaves); err != nil {
		return nil, err
	}
	for purpose, leaf := range leaves {
		sr, err := parseStageRoute(purpose, leaf)
		if err != nil {
			return nil, err
		}
		out[spec.Purpose(purpose)] = sr
	}
	return out, nil
}

// collectStageLeaves finds the maps that hold a stage's default and rules. A map
// is a leaf when it has a default or rules key or any non-map value, so a
// misspelt key is reported as an unknown key rather than a nested purpose.
func collectStageLeaves(m map[string]any, prefix string, out map[string]map[string]any) error {
	for key, value := range m {
		purpose := prefix + key
		sub, isMap := value.(map[string]any)
		if !isMap || isStageLeaf(sub) {
			if !isMap {
				return fmt.Errorf("stages.%s must be a map", purpose)
			}
			out[purpose] = sub
			continue
		}
		if err := collectStageLeaves(sub, purpose+".", out); err != nil {
			return err
		}
	}
	return nil
}

func isStageLeaf(m map[string]any) bool {
	if _, ok := m["default"]; ok {
		return true
	}
	if _, ok := m["rules"]; ok {
		return true
	}
	for _, value := range m {
		if _, isMap := value.(map[string]any); !isMap {
			return true
		}
	}
	return false
}

func parseStageRoute(purpose string, leaf map[string]any) (route.StageRoute, error) {
	where := "stages." + purpose
	if err := onlyKeys(leaf, where, "default", "rules"); err != nil {
		return route.StageRoute{}, err
	}
	sr := route.StageRoute{}
	if raw, ok := leaf["default"]; ok {
		text, isString := raw.(string)
		if !isString {
			return route.StageRoute{}, fmt.Errorf("%s.default must be a string", where)
		}
		sr.Default = strings.TrimSpace(text)
	}
	rules, err := parseRules(where, leaf["rules"])
	if err != nil {
		return route.StageRoute{}, err
	}
	sr.Rules = rules
	return sr, nil
}

func parseRules(where string, raw any) ([]route.Rule, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s.rules must be a list", where)
	}
	rules := make([]route.Rule, 0, len(items))
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.rules[%d] must be a map", where, i)
		}
		at := fmt.Sprintf("%s.rules[%d]", where, i)
		if err := onlyKeys(entry, at, "when", "use"); err != nil {
			return nil, err
		}
		match, err := parseMatch(at, entry["when"])
		if err != nil {
			return nil, err
		}
		use := ""
		if raw, ok := entry["use"]; ok {
			text, isString := raw.(string)
			if !isString {
				return nil, fmt.Errorf("%s: use must be a string", at)
			}
			use = strings.TrimSpace(text)
		}
		rules = append(rules, route.Rule{Match: match, Use: use})
	}
	return rules, nil
}

func parseMatch(at string, raw any) (route.Match, error) {
	var m route.Match
	if raw == nil {
		return m, nil
	}
	when, ok := raw.(map[string]any)
	if !ok {
		return m, fmt.Errorf("%s.when must be a map", at)
	}
	if err := onlyKeys(when, at+".when", "requires_workspace_change"); err != nil {
		return m, err
	}
	if value, ok := when["requires_workspace_change"]; ok {
		b, isBool := value.(bool)
		if !isBool {
			return m, fmt.Errorf("%s.when: requires_workspace_change must be true or false", at)
		}
		m.RequiresWorkspaceChange = &b
	}
	return m, nil
}

// onlyKeys rejects a key the section does not define, so a misspelt key cannot
// be silently ignored (viper ignores unknown keys).
func onlyKeys(m map[string]any, where string, allowed ...string) error {
	for key := range m {
		if !containsString(allowed, key) {
			return fmt.Errorf("%s: unknown key %q", where, key)
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
