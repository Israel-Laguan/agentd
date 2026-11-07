//go:build !fakeacp

package main

import (
	"testing"

	"agentd/internal/agent/acp/fakeagent"
)

// Only the devenv image (-tags fakeacp) serves the fake agent.
func TestProductionBuildHasNoFakeACPAgentSubcommand(t *testing.T) {
	cmd, _, err := newRootCommand().Find([]string{fakeagent.Arg})
	if cmd != nil && err == nil && cmd.Name() == fakeagent.Arg {
		t.Fatal("a build without -tags fakeacp serves the fake agent subcommand")
	}
	// Cobra's Find returns the root command on failure; anything other than the
	// hidden fake-agent subcommand means the production build is clean.
	if cmd != nil && err == nil && cmd.Hidden && cmd.Name() == fakeagent.Arg {
		t.Fatal("a build without -tags fakeacp serves the hidden fake agent subcommand")
	}
}
