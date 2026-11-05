package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"agentd/internal/agent/acp"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	"agentd/internal/paths"
)

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
		if _, removed := entry["level"]; removed {
			return nil, fmt.Errorf("agents.%s: level was removed; the kind sets it (model is L1, acp is L3), so delete it", name)
		}
		agent, err := parseAgent("agents."+name, entry)
		if err != nil {
			return nil, err
		}
		out[name] = agent
	}
	return out, nil
}

// parseAgent reads one entry. The kind fixes the level, so a config cannot make
// a plain model claim L3: a model agent names a provider, an acp agent a preset.
func parseAgent(where string, entry map[string]any) (route.Agent, error) {
	kind, err := optionalString(where, entry, "kind")
	if err != nil {
		return route.Agent{}, err
	}
	if kind == "" {
		kind = route.KindModel
	}
	a := route.Agent{Kind: kind}
	switch kind {
	case route.KindModel:
		a.Level = spec.LevelL1
		if err := onlyKeys(entry, where, "kind", "provider", "model"); err != nil {
			return route.Agent{}, err
		}
		if a.Provider, err = optionalString(where, entry, "provider"); err != nil {
			return route.Agent{}, err
		}
		if a.Provider == "" {
			return route.Agent{}, fmt.Errorf("%s: provider is required", where)
		}
		a.Model, err = optionalString(where, entry, "model")
		return a, err
	case route.KindACP:
		return parseACPAgent(where, entry, a)
	}
	return route.Agent{}, fmt.Errorf("%s: unknown kind %q (want %s or %s)", where, kind, route.KindModel, route.KindACP)
}

// parseACPAgent reads an acp agent: its preset, the model it must run, and
// where the user's binary and login are when not in the preset's defaults.
func parseACPAgent(where string, entry map[string]any, a route.Agent) (route.Agent, error) {
	a.Level = spec.LevelL3
	if err := onlyKeys(entry, where, "kind", "preset", "model", "executable", "credentials"); err != nil {
		return route.Agent{}, err
	}
	var err error
	for key, field := range map[string]*string{"preset": &a.Preset, "model": &a.Model, "executable": &a.Executable, "credentials": &a.Credentials} {
		if *field, err = optionalString(where, entry, key); err != nil {
			return route.Agent{}, err
		}
	}
	preset, ok := acp.Presets[a.Preset]
	if !ok {
		return route.Agent{}, fmt.Errorf("%s: unknown preset %q", where, a.Preset)
	}
	// A preset's own default model may be paid (cline) or not the one wanted.
	if a.Model == "" {
		return route.Agent{}, fmt.Errorf("%s: model is required for an acp agent; name a free one", where)
	}
	if preset.ModelCheck != nil {
		// The preset's model shape is validated here, so a mistake is reported
		// once at load, not when each call starts.
		if err := preset.ModelCheck(a.Model); err != nil {
			return route.Agent{}, fmt.Errorf("%s: %w", where, err)
		}
	}
	if a.Executable != "" && !strings.Contains(a.Executable, "/") {
		// Preset.Executable resolves a bare name against the daemon's cwd, which
		// is not the agent's; leave executable unset for the preset's command on
		// PATH, which Exec looks up.
		return route.Agent{}, fmt.Errorf("%s: executable %q is a bare name, not a path; give its path, or leave executable unset to use the preset's command on PATH", where, a.Executable)
	}
	a.Executable, a.Credentials = paths.ExpandTildePrefix(a.Executable), paths.ExpandTildePrefix(a.Credentials)
	return a, nil
}

func optionalString(where string, entry map[string]any, key string) (string, error) {
	raw, ok := entry[key]
	if !ok {
		return "", nil
	}
	text, isString := raw.(string)
	if !isString {
		return "", fmt.Errorf("%s: %s must be a string", where, key)
	}
	return strings.TrimSpace(text), nil
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
			if _, dup := out[purpose]; dup {
				return fmt.Errorf("stages.%s is defined twice (dotted and nested forms)", purpose)
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
	if len(m) == 0 {
		return true
	}
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
		if err := onlyKeys(entry, at, "when", "use", "min_level"); err != nil {
			return nil, err
		}
		match, err := parseMatch(at, entry["when"])
		if err != nil {
			return nil, err
		}
		use, err := optionalString(at, entry, "use")
		if err != nil {
			return nil, err
		}
		minLevel, err := optionalString(at, entry, "min_level")
		if err != nil {
			return nil, err
		}
		rules = append(rules, route.Rule{Match: match, Use: use, MinLevel: spec.Level(minLevel)})
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
