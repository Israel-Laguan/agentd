package acp

import (
	"context"
	"errors"
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
	_, err = call(t, d, Request{}, fakeagent.Step{Stop: acpsdk.StopReasonCancelled})
	if err == nil || errors.Is(err, ErrPermissionRefused) {
		t.Fatalf("agent cancelled with no refusal: err = %v; want a plain failure", err)
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
	failing := func(context.Context, Permission) (Verdict, error) { return Verdict{Allow: true}, errors.New("store down") }
	r, err := call(t, fakeDriver(t, Config{}), Request{Permit: failing}, askStep(acpsdk.ToolKindExecute, goTest))
	if err != nil || r.Text != "permission: reject_once" {
		t.Fatalf("err = %v, text = %q; want a failed check to reject", err, r.Text)
	}
}
