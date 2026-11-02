package main

import (
	"github.com/spf13/cobra"

	"agentd/internal/sandbox/egress"
)

// newJailForwardCommand is the in-jail half of the egress bridge (T-042): the
// jail has only its own loopback, so this copies each connection on ADDR to
// the host unix SOCKET bound into the jail. The sandbox starts it before an
// external agent; it is not for direct use.
func newJailForwardCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "jail-forward ADDR=SOCKET...",
		Short:  "Forward jail loopback ports to unix sockets (run inside the jail)",
		Hidden: true,
		Args:   cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return egress.Forward(cmd.Context(), cmd.OutOrStdout(), args)
		},
	}
}
