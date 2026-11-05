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
	if p.Tool == "bash" {
		p.Command = commandOf(call.RawInput)
		args, _ := json.Marshal(map[string]string{"command": p.Command})
		p.Args = string(args)
	}
	return p
}

// newPermissions is call as the hooks check it: one Permission per file a
// read or write names (cline's read_files takes several), any other call as
// one. A search with no path reads the workspace; any other call with no
// path is one Permission without it, which the schema check refuses.
func newPermissions(call acpsdk.ToolCallUpdate) []Permission {
	p := newPermission(call)
	if p.Tool != "read" && p.Tool != "write" {
		return []Permission{p}
	}
	paths := pathsOf(call.RawInput)
	switch {
	case len(paths) > 0:
	case p.Kind == string(acpsdk.ToolKindSearch):
		paths = []string{"."} // a search that names no path searches the workspace
	default:
		paths = []string{""}
	}
	in, _ := call.RawInput.(map[string]any)
	perms := make([]Permission, len(paths))
	for i, path := range paths {
		perms[i] = p
		perms[i].Args = fileArgs(p.Tool, path, in)
	}
	return perms
}

// fileArgs is one file of a read or write in agentd's shape, {path} or
// {path, content}. Agents name them their own way: opencode's edit sends
// filePath, oldString and newString.
func fileArgs(tool, path string, in map[string]any) string {
	args := map[string]string{}
	if path != "" {
		args["path"] = path
	}
	if tool == "write" {
		// A write without content is malformed and must reach the schema check,
		// which refuses it; an alias that was absent is not written as "".
		if content, ok := in["content"].(string); ok {
			args["content"] = content
		} else if content, ok := in["newString"].(string); ok {
			args["content"] = content
		} else if content, ok := in["new_string"].(string); ok {
			args["content"] = content
		}
	}
	out, _ := json.Marshal(args)
	return string(out)
}

// pathsOf reads the files a call names: a path string, a list of them or of
// {path} entries, cline's {files}, {file_paths} or {paths}, or one entry
// whose path is path, filePath or file_path.
func pathsOf(raw any) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []any:
		var paths []string
		for _, item := range v {
			paths = append(paths, pathsOf(item)...)
		}
		return paths
	case map[string]any:
		for _, key := range []string{"files", "file_paths", "paths"} {
			if list, ok := v[key]; ok {
				return pathsOf(list)
			}
		}
		if path := firstString(v, "path", "filePath", "file_path"); path != "" {
			return []string{path}
		}
	}
	return nil
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

// agentdSessionTools are the MCP tools agentd attaches to a session, named
// server__tool by the agents that send them as kind other; the server is
// agentd's own, so the tool names read agentd__<tool>. The driver lets its own
// through without a hook, and refuses any other kind other call. The board.*
// entries are project-scoped read-only tools (tasks, projects, comments).
var agentdSessionTools = []string{"report_outcome", "get_context", "ask_human", "board.list_tasks", "board.get_task", "board.list_projects", "board.get_project", "board.list_comments"}

// ownTool reports whether p calls one of agentd's own session tools, which the
// driver allows without a hook: the session tools touch only agentd's session,
// and the board.* tools are project-scoped reads. The names must match exactly,
// never a prefix, so an agent that names an unmapped kind with a title of its
// own cannot ride the bypass.
func ownTool(p Permission, servers []acpsdk.McpServer) bool {
	if p.Tool != "" {
		return false
	}
	for _, s := range servers {
		if s.Http == nil || s.Http.Name != "agentd" {
			continue
		}
		for _, t := range agentdSessionTools {
			if p.Title == s.Http.Name+"__"+t {
				return true
			}
		}
	}
	return false
}

// decideAll answers every part of one request; the first refusal answers it.
func decideAll(ctx context.Context, permit Permitter, perms []Permission, readOnly, noWorkspace bool) Verdict {
	var v Verdict
	for _, p := range perms {
		if v = decide(ctx, permit, p, readOnly, noWorkspace); !v.Allow || v.Suspend {
			return v
		}
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
