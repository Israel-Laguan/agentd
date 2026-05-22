package main

import "github.com/spf13/cobra"

func newSuggestCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "suggest-task <task-id>",
		Short: "Ask the AI gateway for a human-run task command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, deps, cleanup, err := openRuntime(opts)
			if err != nil {
				return err
			}
			defer cleanup()
			if err := requireStartupProviders(cfg.Gateway); err != nil {
				return err
			}
			suggestion, err := deps.runner.Suggest(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeFormat(cmd.OutOrStdout(), "Suggested command (run yourself):\n  %s\n", suggestion)
		},
	}
}
