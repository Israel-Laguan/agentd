//go:build fakeacp

package main

import (
	"testing"

	"agentd/internal/agent/acp/fakeagent"
)

func TestFakeACPAgentSubcommandIsHidden(t *testing.T) {
	cmd, _, err := newRootCommand().Find([]string{fakeagent.Arg})
	if err != nil || cmd.Name() != fakeagent.Arg {
		t.Fatalf("Find(%q) = %q, %v; want the fake agent subcommand", fakeagent.Arg, cmd.Name(), err)
	}
	if !cmd.Hidden || !cmd.DisableFlagParsing {
		t.Fatalf("hidden = %v, flag parsing disabled = %v; want both (fakeagent parses its own flags)", cmd.Hidden, cmd.DisableFlagParsing)
	}
}
