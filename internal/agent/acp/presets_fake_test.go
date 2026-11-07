//go:build fakeacp

package acp

import (
	"slices"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
)

// The devenv build runs the fake preset as agentd's own hidden subcommand, in
// the jail with no egress (T-069).
func TestFakePresetRunsAgentdsFakeAgentSubcommand(t *testing.T) {
	p, ok := Presets["fake"]
	if !ok {
		t.Fatal("the fakeacp build has no fake preset")
	}
	if p.Command != "agentd" || len(p.Allow) != 0 || !p.NoUsage || p.ModelOption {
		t.Fatalf("fake preset = %+v, want agentd on PATH, empty egress allowlist (no external hosts), no usage, no model option", p)
	}
	// p.Allow carries host:port entries for the jail's egress allowlist; empty
	// means no external host is reachable, not implicit allow.
	exe := t.TempDir() + "/agentd"
	argv, err := p.Home(Launch{Home: t.TempDir(), Executable: exe})
	if err != nil {
		t.Fatal(err)
	}
	// Argv is [executable, fakeagent.Arg]; only the suffix is pinned, the prefix
	// is the caller-supplied path.
	if !slices.Equal(argv, []string{exe, fakeagent.Arg}) {
		t.Fatalf("argv = %q, want %q", argv, []string{exe, fakeagent.Arg})
	}
}
