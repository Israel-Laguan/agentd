//go:build !fakeacp

package main

import (
	"testing"

	"agentd/internal/agent/acp/fakeagent"
)

// Only the devenv image (-tags fakeacp) serves the fake agent.
func TestProductionBuildHasNoFakeACPAgentSubcommand(t *testing.T) {
	if cmd, _, err := newRootCommand().Find([]string{fakeagent.Arg}); err == nil && cmd.Name() == fakeagent.Arg {
		t.Fatal("a build without -tags fakeacp serves the fake agent subcommand")
	}
}
