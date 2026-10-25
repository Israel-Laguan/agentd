package hooks

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// dangerousCommands match risky commands at a command boundary (start of the
// line or after whitespace, ;, &, |, parentheses or a backtick), so "sudo ls" is
// refused but "grep pseudocode" and "rm -rf /tmp/build" are not. This is a
// best-effort hint, not a security boundary.
var dangerousCommands = []struct {
	name string
	re   *regexp.Regexp
}{
	{"sudo", commandRE(`sudo\b`)},
	{"su -", commandRE(`su\s+-`)},
	{"rm -rf /", commandRE(`rm\s+-[a-z]*(r[a-z]*f|f[a-z]*r)[a-z]*\s+/(\s|\*|$)`)},
	{"mkfs", commandRE(`mkfs\b`)},
	{"dd if=", commandRE(`dd\s+if=`)},
	{":(){:|:&};:", regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`)},
}

// DenylistSudoReason is the veto reason the denylist gives a sudo command. The
// worker reads it as the sudo refusal, which is a privilege block.
const DenylistSudoReason = `command blocked: contains dangerous pattern "sudo"`

func commandRE(pattern string) *regexp.Regexp {
	const boundary = "(^|[\\s;&|()`])"
	full := "(?i)" + boundary + pattern
	return regexp.MustCompile(full)
}

// DenylistHook returns a PreHook that blocks dangerous commands for bash
// and path-traversal attempts for file-system tools. The allowedRoot is
// resolved once at construction time and used to verify that any argument
// whose key contains "path" stays within bounds.
func DenylistHook(allowedRoot string) PreHook {
	fixedRoot := func(string) string {
		return allowedRoot
	}
	return DenylistHookFor(fixedRoot)
}

// DenylistHookFor is DenylistHook with the allowed root chosen per call from
// the project the tool call belongs to, so one project's tools cannot name a
// path in another project's workspace.
func DenylistHookFor(rootFor func(projectID string) string) PreHook {
	return PreHook{
		Name:   "denylist",
		Policy: FailClosed,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if ctx.ToolName == toolNameBash {
				if v := checkDangerousCommand(ctx.Args); v.Veto {
					return v, nil
				}
			}
			if !plainProjectID(ctx.ProjectID) {
				return HookVerdict{Veto: true, Reason: fmt.Sprintf("project id %q is not a single path element", ctx.ProjectID)}, nil
			}
			if v := checkPathTraversal(ctx.Args, filepath.Clean(rootFor(ctx.ProjectID))); v.Veto {
				return v, nil
			}
			return HookVerdict{}, nil
		},
	}
}

// plainProjectID reports whether id names one directory under the projects
// dir: no separator, not "." or "..". Empty stays allowed (no project).
func plainProjectID(id string) bool {
	if id == "." || id == ".." {
		return false
	}
	return !strings.ContainsAny(id, `/\`)
}

func checkDangerousCommand(argsJSON string) HookVerdict {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return HookVerdict{}
	}
	for _, d := range dangerousCommands {
		if d.re.MatchString(args.Command) {
			return HookVerdict{Veto: true, Reason: fmt.Sprintf("command blocked: contains dangerous pattern %q", d.name)}
		}
	}
	return HookVerdict{}
}

func checkPathTraversal(argsJSON string, root string) HookVerdict {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return HookVerdict{}
	}
	for key, val := range raw {
		if !strings.Contains(strings.ToLower(key), "path") {
			continue
		}
		var pathArg string
		if err := json.Unmarshal(val, &pathArg); err != nil {
			continue
		}
		resolved := filepath.Clean(filepath.Join(root, pathArg))
		if !strings.HasPrefix(resolved, root+string(filepath.Separator)) && resolved != root {
			return HookVerdict{
				Veto:   true,
				Reason: fmt.Sprintf("path %q escapes allowed root %q", pathArg, root),
			}
		}
	}
	return HookVerdict{}
}

// SchemaValidationHook returns a PreHook that validates tool call
// arguments against their registered FunctionParameters JSON Schema.
// The registry maps tool names to their parameter definitions.
func SchemaValidationHook(registry map[string]*gateway.FunctionParameters) PreHook {
	return PreHook{
		Name:   "schema-validation",
		Policy: FailClosed,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			schema, ok := registry[ctx.ToolName]
			if !ok || schema == nil {
				return HookVerdict{}, nil
			}
			if v := validateArgs(ctx.Args, schema); v.Veto {
				return v, nil
			}
			return HookVerdict{}, nil
		},
	}
}

func validateArgs(argsJSON string, schema *gateway.FunctionParameters) HookVerdict {
	if strings.TrimSpace(argsJSON) == "" {
		if len(schema.Required) > 0 {
			return HookVerdict{
				Veto:   true,
				Reason: fmt.Sprintf("missing required arguments: %s", strings.Join(schema.Required, ", ")),
			}
		}
		return HookVerdict{}
	}

	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return HookVerdict{Veto: true, Reason: fmt.Sprintf("arguments are not valid JSON: %v", err)}
	}

	for _, req := range schema.Required {
		if _, ok := args[req]; !ok {
			return HookVerdict{
				Veto:   true,
				Reason: fmt.Sprintf("missing required argument %q", req),
			}
		}
	}

	if schema.Properties != nil {
		for key := range args {
			if _, ok := schema.Properties[key]; !ok {
				return HookVerdict{
					Veto:   true,
					Reason: fmt.Sprintf("unknown argument %q", key),
				}
			}
		}
	}

	for key, val := range args {
		propDef, ok := schema.Properties[key]
		if !ok {
			continue
		}
		propMap, ok := propDef.(map[string]any)
		if !ok {
			continue
		}
		expectedType, ok := propMap["type"].(string)
		if !ok {
			continue
		}
		if v := checkType(key, val, expectedType); v.Veto {
			return v
		}
	}

	return HookVerdict{}
}

// ScrubResultHook returns a PostHook that applies the given Scrubber to tool
// results before they are appended to the model context window. This is
// FailClosed because redaction is security-critical.
func ScrubResultHook(scrubber sandbox.Scrubber) PostHook {
	return PostHook{
		Name:   "scrub-result",
		Policy: FailClosed,
		Fn: func(_ HookContext, result string) (string, error) {
			if scrubber == nil {
				return result, nil
			}
			return scrubber.Scrub(result), nil
		},
	}
}

func checkType(key string, raw json.RawMessage, expected string) HookVerdict {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return HookVerdict{Veto: true, Reason: fmt.Sprintf("argument %q: invalid JSON value", key)}
	}

	ok := false
	switch expected {
	case "string":
		_, ok = v.(string)
	case "number":
		_, ok = v.(float64)
	case "integer":
		f, isFloat := v.(float64)
		ok = isFloat && f == float64(int64(f))
	case "boolean":
		_, ok = v.(bool)
	case "object":
		_, ok = v.(map[string]any)
	case "array":
		_, ok = v.([]any)
	default:
		ok = true
	}

	if !ok {
		return HookVerdict{
			Veto:   true,
			Reason: fmt.Sprintf("argument %q: expected type %q", key, expected),
		}
	}
	return HookVerdict{}
}
