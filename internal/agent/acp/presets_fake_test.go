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
		t.Fatalf("fake preset = %+v, want agentd on PATH, no egress, no usage, no model option", p)
	}
	argv, err := p.Home(Launch{Home: t.TempDir(), Executable: "/usr/local/bin/agentd"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/usr/local/bin/agentd", fakeagent.Arg}; !slices.Equal(argv, want) {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
}
