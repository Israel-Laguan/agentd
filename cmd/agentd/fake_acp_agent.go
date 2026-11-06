//go:build fakeacp

package main

import (
	"os"

	"github.com/spf13/cobra"

	"agentd/internal/agent/acp/fakeagent"
)

// The fake-acp-agent subcommand serves the scripted fake ACP agent on stdin
// and stdout, for the devenv build's fake preset (T-069). Flags are fakeagent's.
func init() {
	extraCommands = append(extraCommands, func() *cobra.Command {
		return &cobra.Command{
			Use:                fakeagent.Arg,
			Short:              "Serve the scripted fake ACP agent (devenv builds only)",
			Hidden:             true,
			DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, args []string) error {
				return fakeagent.Serve(args, os.Stdin, os.Stdout)
			},
		}
	})
}
