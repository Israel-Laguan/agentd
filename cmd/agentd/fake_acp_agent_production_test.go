//go:build !fakeacp

package main

import (
	"testing"

	"agentd/internal/agent/acp/fakeagent"
)

// Only the devenv image (-tags fakeacp) serves the fake agent.
func TestProductionBuildHasNoFakeACPAgentSubcommand(t *testing.T) {
	// Cobra's Find errors when the path is not found and returns the root
	// command on failure; a found hidden fake-agent command is the only
	// production violation.
	cmd, _, err := newRootCommand().Find([]string{fakeagent.Arg})
	if err == nil && cmd != nil && cmd.Name() == fakeagent.Arg && cmd.Hidden {
		t.Fatal("a build without -tags fakeacp serves the hidden fake agent subcommand")
	}
}
