package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	acpsdk "github.com/coder/acp-go-sdk"
)

// Permission is one session/request_permission, named the way agentd names a
// builtin tool call, so the same AllowedTools veto and approval gates answer it.
type Permission struct {
	ToolCallID string
	// Kind is the ACP tool kind the agent declared.
	Kind string
	// Tool is agentd's tool for the kind (agent/tools names): bash, read or write.
	Tool string
	// Args is the call's input as JSON in the builtin tool's shape, so the
	// hooks read it as they read a builtin call: {"command": ...} for bash,
	// {path} for read, {path, content} for write.
	Args string
	// Command is what a bash call runs, as read from the raw input.
	Command string
	Title   string
}

// Verdict answers a Permission. Suspend means the task is now blocked on a
// human (an approval gate's HITL subtask), so the call stops with ErrSuspended.
type Verdict struct {
	Allow   bool
	Suspend bool
	Reason  string
}

// Permitter answers the permission requests of one call. In agentd it runs the
// task's hooks (worker.ACPPermitter).
type Permitter func(ctx context.Context, p Permission) (Verdict, error)

// toolForKind maps an ACP tool kind onto agentd's tool with the same effect.
// Any other kind (fetch, switch_mode, think, other, which is cline's for MCP
// tools) has no tool a hook could gate.
var toolForKind = map[acpsdk.ToolKind]string{
	acpsdk.ToolKindExecute: "bash",
	acpsdk.ToolKindRead:    "read",
	acpsdk.ToolKindSearch:  "read",
	acpsdk.ToolKindEdit:    "write",
	acpsdk.ToolKindDelete:  "write",
	acpsdk.ToolKindMove:    "write",
}

// packageInstall matches a command that fetches a package to install or run:
// npx-style runners, and the install verbs of the usual package managers,
// after any options (`npm --prefix x install`). It is a tripwire for an agent
// that installs in the open, as cline did (SP-022), not a shell parser: a
// quoted or built-up command passes it, and finds no registry on the egress
// allowlist. Text that only names an install (`echo 'npm install'`) is refused
// too; that errs on the closed side.
var packageInstall = regexp.MustCompile(`(?i)\b(?:npx|pnpx|bunx|uvx|pipx)\b` +
	`|\b(?:npm|pnpm|yarn|bun)` + options + `\s+(?:i|install|add|ci|dlx|exec|x)\b` +
	`|\b(?:pip3?|uv\s+pip|python3?\s+-m\s+pip|poetry|gem)` + options + `\s+(?:install|add)\b|\buv` + options + `\s+(?:add|tool)\b` +
	`|\bgo` + options + `\s+(?:install|get)\b|\bcargo` + options + `\s+(?:install|add)\b` +
	`|\b(?:apt|apt-get|dnf|yum|apk|brew|pacman|zypper)` + options + `\s+(?:install|add|-S)\b`)

// options skips a tool's options, each with an optional value, before its verb.
const options = `(?:\s+[-+]\S*(?:\s+[^-\s]\S*)?)*`

func newPermission(call acpsdk.ToolCallUpdate) Permission {
	p := Permission{ToolCallID: string(call.ToolCallId)}
	if call.Kind != nil {
		p.Kind = string(*call.Kind)
		p.Tool = toolForKind[*call.Kind]
	}
	if call.Title != nil {
		p.Title = *call.Title
	}
	if raw, err := json.Marshal(call.RawInput); err == nil && call.RawInput != nil {
		p.Args = string(raw)
	}
	switch p.Tool {
	case "bash":
		p.Command = commandOf(call.RawInput)
		args, _ := json.Marshal(map[string]string{"command": p.Command})
		p.Args = string(args)
	case "read", "write":
		p.Args = fileArgs(p.Tool, call.RawInput)
	}
	return p
}

// fileArgs is a read or write call's input in agentd's shape, {path} or
// {path, content}, which the hooks check. Agents name them their own way:
// opencode's edit sends filePath, oldString and newString. With no path the
// key is left out, so the schema check refuses the call.
func fileArgs(tool string, raw any) string {
	in, _ := raw.(map[string]any)
	args := map[string]string{}
	if path := firstString(in, "path", "filePath", "file_path"); path != "" {
		args["path"] = path
	}
	if tool == "write" {
		args["content"] = firstString(in, "content", "newString", "new_string")
	}
	out, _ := json.Marshal(args)
	return string(out)
}

func firstString(in map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := in[key].(string); ok {
			return s
		}
	}
	return ""
}

// commandOf reads the command from an execute call's raw input, in the shapes
// cline's run_commands takes: a string, a list, or an object with command or
// commands, where an item is a string or {executable, args}. opencode sends
// command, and its bash falls back to cmd.
func commandOf(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var lines []string
		for _, item := range v {
			if line := commandOf(item); line != "" {
				lines = append(lines, line)
			}
		}
		return strings.Join(lines, "\n")
	case map[string]any:
		if exe, ok := v["executable"].(string); ok {
			words := []string{exe}
			args, _ := v["args"].([]any)
			for _, arg := range args {
				words = append(words, fmt.Sprint(arg))
			}
			return strings.TrimSpace(strings.Join(words, " "))
		}
		for _, key := range []string{"command", "cmd", "commands"} {
			if command, ok := v[key]; ok {
				return commandOf(command)
			}
		}
	}
	return ""
}

// decide answers p for one call. Only a call the driver can name reaches the
// Permitter: an unknown kind, or a bash call whose command it cannot read,
// would run under a name no hook gates. Package installs are refused outright,
// since an approve-all client ran `npx -y <unknown package>` (docs/acp-agents.md).
// A read-only session refuses every write, and a no-workspace session refuses
// every call. With no Permitter every call is refused.
func decide(ctx context.Context, permit Permitter, p Permission, readOnly, noWorkspace bool) Verdict {
	switch {
	case noWorkspace:
		return Verdict{Reason: "the stage has no workspace access"}
	case p.Tool == "":
		return Verdict{Reason: fmt.Sprintf("tool kind %q is not one agentd can check", p.Kind)}
	case readOnly && p.Tool == "write":
		return Verdict{Reason: "the session is read-only"}
	case p.Tool == "bash" && p.Command == "":
		return Verdict{Reason: "the command is unknown"}
	case p.Tool == "bash" && packageInstall.MatchString(p.Command):
		return Verdict{Reason: "package installs are not allowed"}
	case permit == nil:
		return Verdict{Reason: "no permission check is configured"}
	}
	v, err := permit(ctx, p)
	if err != nil {
		return Verdict{Reason: "permission check failed: " + err.Error()}
	}
	return v
}

// outcome selects allow_once for an allowed call and reject_once for a refused
// one, never an always option: allow_always would let the agent skip every
// later check, and reject_always refuse a later call the hooks would allow.
// With no such option the request is cancelled, which also refuses it.
func outcome(options []acpsdk.PermissionOption, allow bool) acpsdk.RequestPermissionOutcome {
	kind := acpsdk.PermissionOptionKindRejectOnce
	if allow {
		kind = acpsdk.PermissionOptionKindAllowOnce
	}
	for _, option := range options {
		if option.Kind == kind {
			return acpsdk.RequestPermissionOutcome{Selected: &acpsdk.RequestPermissionOutcomeSelected{OptionId: option.OptionId}}
		}
	}
	return acpsdk.RequestPermissionOutcome{Cancelled: &acpsdk.RequestPermissionOutcomeCancelled{}}
}
