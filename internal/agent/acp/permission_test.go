package acp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"agentd/internal/agent/acp/fakeagent"

	acpsdk "github.com/coder/acp-go-sdk"
)

// recorder is a Permitter that answers with verdict and keeps what it saw.
type recorder struct {
	verdict Verdict
	mu      sync.Mutex
	seen    []Permission
}

func (r *recorder) permit(_ context.Context, p Permission) (Verdict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, p)
	return r.verdict, nil
}

func (r *recorder) calls() []Permission {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Permission(nil), r.seen...)
}

func askStep(kind acpsdk.ToolKind, rawInput any) fakeagent.Step {
	return fakeagent.Step{Permission: &fakeagent.Permission{Kind: kind, Title: "tool", RawInput: rawInput}}
}

var goTest = map[string]any{"command": "go test ./..."}

func TestPermissionApprovedByTheHooksAllowsOnce(t *testing.T) {
	t.Parallel()
	hooks := &recorder{verdict: Verdict{Allow: true}}
	r, err := call(t, fakeDriver(t, Config{}), Request{Permit: hooks.permit}, askStep(acpsdk.ToolKindExecute, goTest))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	// Never allow_always: the agent would stop asking, and the next call would
	// skip the hooks.
	if r.Text != "permission: allow_once" {
		t.Fatalf("text = %q; want allow_once picked", r.Text)
	}
	seen := hooks.calls()
	if len(seen) != 1 || seen[0].Tool != "bash" || seen[0].Command != "go test ./..." || seen[0].Args != `{"command":"go test ./..."}` {
		t.Fatalf("hooks saw %+v; want one bash call with its command and raw arguments", seen)
	}
}

// The hooks read a bash call's command from {"command": ...}, the builtin bash
// tool's input, so a cline string or list reaches them in that shape.
func TestBashArgsTakeTheBuiltinShape(t *testing.T) {
	t.Parallel()
	hooks := &recorder{verdict: Verdict{Allow: true}}
	_, err := call(t, fakeDriver(t, Config{}), Request{Permit: hooks.permit}, askStep(acpsdk.ToolKindExecute, []any{"go vet ./...", "go test ./..."}))
	if seen := hooks.calls(); err != nil || len(seen) != 1 || seen[0].Args != `{"command":"go vet ./...\ngo test ./..."}` {
		t.Fatalf("err = %v, hooks saw %+v; want Args as {\"command\": ...}", err, seen)
	}
}

// The hooks check agentd's read and write inputs, {path} and {path, content}.
// opencode's edit sends filePath, oldString and newString (live smoke,
// 2026-11-05), which the schema check refused as a write without content.
func TestFileArgsTakeTheBuiltinShape(t *testing.T) {
	t.Parallel()
	hooks := &recorder{verdict: Verdict{Allow: true}}
	_, err := call(t, fakeDriver(t, Config{}), Request{Permit: hooks.permit},
		askStep(acpsdk.ToolKindEdit, map[string]any{"filePath": "main.go", "oldString": "a", "newString": "b"}),
		askStep(acpsdk.ToolKindRead, map[string]any{"path": "go.mod"}))
	seen := hooks.calls()
	if err != nil || len(seen) != 2 || seen[0].Args != `{"content":"b","path":"main.go"}` || seen[1].Args != `{"path":"go.mod"}` {
		t.Fatalf("err = %v, hooks saw %+v; want agentd's write and read shapes", err, seen)
	}
}

// cline's read_files names several files in one request, in several shapes;
// each path is a read the hooks check, and one refusal refuses the request.
func TestEveryPathOfAMultiFileReadPassesTheHooks(t *testing.T) {
	t.Parallel()
	hooks := &recorder{verdict: Verdict{Allow: true}}
	_, err := call(t, fakeDriver(t, Config{}), Request{Permit: hooks.permit},
		askStep(acpsdk.ToolKindRead, map[string]any{"files": []any{map[string]any{"path": "/ws/a.go", "start_line": 1}, "/ws/b.go"}}),
		askStep(acpsdk.ToolKindRead, []any{"/ws/c.go"}),
		// cline's search_codebase names no path: it searches the workspace.
		askStep(acpsdk.ToolKindSearch, map[string]any{"queries": []any{"CountWords"}}))
	var args []string
	for _, p := range hooks.calls() {
		args = append(args, p.Args)
	}
	want := []string{`{"path":"/ws/a.go"}`, `{"path":"/ws/b.go"}`, `{"path":"/ws/c.go"}`, `{"path":"."}`}
	if err != nil || !slices.Equal(args, want) {
		t.Fatalf("err = %v, hooks saw %q; want %q", err, args, want)
	}
}

// cline asks before every tool, its MCP tools as kind other titled
// server__tool (live smoke, 2026-11-05). agentd's own server's tools are
// allowed with no hook, in a no-workspace session too; another server's are not.
func TestOwnMCPToolsAreAllowedAndOthersRefused(t *testing.T) {
	t.Parallel()
	own := []acpsdk.McpServer{{Http: &acpsdk.McpServerHttpInline{Name: "agentd", Type: "http", Url: "http://x", Headers: []acpsdk.HttpHeader{}}}}
	ask := func(title string) fakeagent.Step {
		return fakeagent.Step{Permission: &fakeagent.Permission{Kind: acpsdk.ToolKindOther, Title: title, RawInput: map[string]any{}}}
	}
	r, err := call(t, fakeDriver(t, Config{}), Request{MCPServers: own, NoWorkspace: true}, ask("agentd__report_outcome"))
	if err != nil || r.Text != "permission: allow_once" {
		t.Fatalf("agentd's own tool: %q, %v; want it allowed once", r.Text, err)
	}
	r, err = call(t, fakeDriver(t, Config{}), Request{MCPServers: own}, ask("composio__search_tools"))
	if err != nil || r.Text != "permission: reject_once" {
		t.Fatalf("another server's tool: %q, %v; want it rejected", r.Text, err)
	}
}

func TestPermissionDeniedByTheHooksRejects(t *testing.T) {
	t.Parallel()
	hooks := &recorder{verdict: Verdict{Reason: "tool \"write\" is not in this profile's allowed_tools"}}
	r, err := call(t, fakeDriver(t, Config{}), Request{Permit: hooks.permit}, askStep(acpsdk.ToolKindEdit, map[string]any{"path": "main.go"}))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if r.Text != "permission: reject_once" {
		t.Fatalf("text = %q; want the call rejected", r.Text)
	}
	if seen := hooks.calls(); len(seen) != 1 || seen[0].Tool != "write" {
		t.Fatalf("hooks saw %+v; want one write call", seen)
	}
}

// opencode answers a rejected permission by ending the turn cancelled, with no
// error (live, v2.2.5). That turn did not do its work, so the call fails.
func TestTurnEndedAfterARefusalFails(t *testing.T) {
	t.Parallel()
	d := fakeDriver(t, Config{})
	hooks := &recorder{verdict: Verdict{Reason: "tool \"bash\" is not in this profile's allowed_tools"}}
	r, err := call(t, d, Request{Permit: hooks.permit}, askStep(acpsdk.ToolKindExecute, goTest), fakeagent.Step{Stop: acpsdk.StopReasonCancelled})
	if !errors.Is(err, ErrPermissionRefused) || !strings.Contains(err.Error(), "allowed_tools") || r.StopReason != acpsdk.StopReasonCancelled {
		t.Fatalf("err = %v, stop = %q; want ErrPermissionRefused naming the hook's reason, and the stop reason kept", err, r.StopReason)
	}
	silent := &recorder{verdict: Verdict{}}
	_, err = call(t, d, Request{Permit: silent.permit}, askStep(acpsdk.ToolKindExecute, goTest), fakeagent.Step{Stop: acpsdk.StopReasonCancelled})
	if !errors.Is(err, ErrPermissionRefused) {
		t.Fatalf("refusal with no reason: err = %v; want ErrPermissionRefused", err)
	}
	_, err = call(t, d, Request{}, fakeagent.Step{Stop: acpsdk.StopReasonCancelled})
	if err == nil || errors.Is(err, ErrPermissionRefused) {
		t.Fatalf("agent cancelled with no refusal: err = %v; want a plain failure", err)
	}
}

// reject_always could let the agent refuse a later call itself, one the hooks
// would allow, so a refusal without reject_once cancels the request.
func TestRefusalNeverPicksRejectAlways(t *testing.T) {
	t.Parallel()
	options := []acpsdk.PermissionOption{
		{OptionId: "always", Kind: acpsdk.PermissionOptionKindAllowAlways},
		{OptionId: "never", Kind: acpsdk.PermissionOptionKindRejectAlways},
	}
	if got := outcome(options, false); got.Cancelled == nil {
		t.Fatalf("outcome = %+v; want the request cancelled", got)
	}
}

// The tripwire looks for an install verb after the tool's options, and leaves
// the tool's other verbs alone.
func TestPackageInstallSkipsOptionsBeforeTheVerb(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"npm --silent install left-pad", "npm --prefix /tmp install left-pad", "apt-get -y install curl", "pip --quiet install requests", "python3 -m pip install requests"} {
		if !packageInstall.MatchString(command) {
			t.Errorf("%q: not seen as a package install", command)
		}
	}
	for _, command := range []string{"go -C sub test ./...", "npm run build", "go vet ./...", "npm --version", "python -m pip list"} {
		if packageInstall.MatchString(command) {
			t.Errorf("%q: seen as a package install", command)
		}
	}
}

// opencode's bash reads cmd when command is missing.
func TestCmdIsTheCommandWhenCommandIsMissing(t *testing.T) {
	t.Parallel()
	if got := commandOf(map[string]any{"cmd": "go test ./..."}); got != "go test ./..." {
		t.Fatalf("commandOf = %q; want the cmd value", got)
	}
}

func TestNoPermitterRejectsEverything(t *testing.T) {
	t.Parallel()
	r, err := call(t, fakeDriver(t, Config{}), Request{}, askStep(acpsdk.ToolKindRead, map[string]any{"path": "go.mod"}))
	if err != nil || r.Text != "permission: reject_once" {
		t.Fatalf("err = %v, text = %q; want a rejection with no hooks to ask", err, r.Text)
	}
}

// Package installs and calls the driver cannot name are refused before any
// hook runs: a client that approves everything approves `npx -y <unknown>`.
func TestPackageInstallsAndUnknownCallsAreRejectedBeforeTheHooks(t *testing.T) {
	t.Parallel()
	d := fakeDriver(t, Config{})
	for name, step := range map[string]fakeagent.Step{
		"npx":             askStep(acpsdk.ToolKindExecute, map[string]any{"command": "npx -y @agentd/mcp report_outcome"}),
		"cline commands":  askStep(acpsdk.ToolKindExecute, map[string]any{"commands": []any{"ls", "pip install requests"}}),
		"cline argv":      askStep(acpsdk.ToolKindExecute, []any{map[string]any{"executable": "npm", "args": []any{"install", "left-pad"}}}),
		"go get":          askStep(acpsdk.ToolKindExecute, "go get example.com/cloudlines"),
		"no command":      askStep(acpsdk.ToolKindExecute, map[string]any{"cwd": "/w"}),
		"unknown kind":    askStep(acpsdk.ToolKindOther, map[string]any{"name": "skills"}),
		"mode switch":     askStep(acpsdk.ToolKindSwitchMode, map[string]any{"mode": "act"}),
		"web fetch":       askStep(acpsdk.ToolKindFetch, map[string]any{"url": "https://example.com"}),
		"mcp tool (kind)": fakeagent.Step{Permission: &fakeagent.Permission{Title: "composio_search_tools"}},
	} {
		hooks := &recorder{verdict: Verdict{Allow: true}}
		r, err := call(t, d, Request{Permit: hooks.permit}, step)
		if err != nil || r.Text != "permission: reject_once" {
			t.Errorf("%s: err = %v, text = %q; want it rejected", name, err, r.Text)
		}
		if seen := hooks.calls(); len(seen) != 0 {
			t.Errorf("%s: hooks saw %+v; want no hook asked", name, seen)
		}
	}
}

// An approval gate that blocked the task on a human (HITL) stops the call: the
// agent must not go on without the approval.
func TestSuspendingVerdictStopsTheCall(t *testing.T) {
	t.Parallel()
	hooks := &recorder{verdict: Verdict{Suspend: true, Reason: "paused pending human approval"}}
	_, err := call(t, fakeDriver(t, Config{}), Request{Permit: hooks.permit},
		askStep(acpsdk.ToolKindExecute, goTest), fakeagent.Step{WaitCancel: true})
	if !errors.Is(err, ErrSuspended) {
		t.Fatalf("err = %v; want ErrSuspended", err)
	}
}

func TestPermitterErrorRejects(t *testing.T) {
	t.Parallel()
	failing := func(context.Context, Permission) (Verdict, error) {
		return Verdict{Allow: true}, errors.New("store down")
	}
	r, err := call(t, fakeDriver(t, Config{}), Request{Permit: failing}, askStep(acpsdk.ToolKindExecute, goTest))
	if err != nil || r.Text != "permission: reject_once" {
		t.Fatalf("err = %v, text = %q; want a failed check to reject", err, r.Text)
	}
}
