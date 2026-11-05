package fakeagent

import (
	"encoding/json"
	"reflect"
	"strings"
)

// scriptFromPrompt finds the test script in a prompt: the whole prompt, or one
// line of a context pack, which agentd prefixes with its own messages.
// See parseScriptLine in script.go for the strict line matching.
func scriptFromPrompt(text string) []Step {
	var steps []Step
	if json.NewDecoder(strings.NewReader(text)).Decode(&steps) == nil && steps != nil {
		return steps
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		if candidate, ok := parseScriptLine(line); ok {
			return candidate
		}
	}
	return nil
}

// parseScriptLine accepts a context-pack line only when it is a non-empty list
// of non-empty steps; a stray JSON array in the pack is not a script.
func parseScriptLine(line string) ([]Step, bool) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	var steps []Step
	if err := dec.Decode(&steps); err != nil || len(steps) == 0 {
		return nil, false
	}
	for _, step := range steps {
		if reflect.DeepEqual(step, Step{}) {
			return nil, false
		}
	}
	return steps, true
}
