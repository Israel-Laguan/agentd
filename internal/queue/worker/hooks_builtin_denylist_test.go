package worker

import (
	"encoding/json"
	"testing"
	"time"

	agenthooks "agentd/internal/agent/hooks"
)

func denylistVerdict(t *testing.T, hook agenthooks.PreHook, projectID, args string) agenthooks.HookVerdict {
	t.Helper()
	verdict, err := hook.Fn(agenthooks.HookContext{ToolName: "bash", ProjectID: projectID, Args: args, Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return verdict
}

// A pattern inside a longer word is not the command: "pseudocode" is not sudo.
func TestDenylistHook_MatchesCommandsNotSubstrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		command string
		blocked bool
	}{
		{`grep -rn pseudocode docs`, false},
		{`cat addendum.md`, false},
		{`rm -rf /tmp/build`, false},
		{`rm -rf ./build /var/tmp/x`, false},
		{`sudo ls`, true},
		{`ls && sudo id`, true},
		{`echo hi | sudo tee /etc/x`, true},
		{`(sudo id)`, true},
		{`rm -rf /`, true},
		{`rm -rf / --no-preserve-root`, true},
		{`rm -rf /*`, true},
		{`mkfs.ext4 /dev/sda1`, true},
		{`su - root`, true},
		{`dd if=/dev/zero of=/dev/sda`, true},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			t.Parallel()
			verdict := denylistVerdict(t, agenthooks.DenylistHook("/workspace"), "", `{"command":`+quote(tt.command)+`}`)
			if verdict.Veto != tt.blocked {
				t.Fatalf("Veto = %v, want %v (reason: %s)", verdict.Veto, tt.blocked, verdict.Reason)
			}
		})
	}
}

// A project's tools may reach its own workspace, not a sibling project's.
func TestDenylistHookFor_RootsPathsAtTheProjectWorkspace(t *testing.T) {
	t.Parallel()
	hook := agenthooks.DenylistHookFor(func(projectID string) string { return "/projects/" + projectID })
	if v := denylistVerdict(t, hook, "p1", `{"path":"src/main.go"}`); v.Veto {
		t.Fatalf("own workspace path refused: %s", v.Reason)
	}
	if v := denylistVerdict(t, hook, "p1", `{"path":"../p2/secret"}`); !v.Veto {
		t.Fatal("a path into a sibling project was not refused")
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
