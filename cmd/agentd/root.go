package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

type rootOptions struct {
	home       string
	configFile string
	verbose    bool
}

func newRootCommand() *cobra.Command {
	opts := &rootOptions{}

	rootCmd := &cobra.Command{
		Use:   "agentd",
		Short: "Local-first autonomous workforce daemon",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logLevel := slog.LevelInfo
			if opts.verbose {
				logLevel = slog.LevelDebug
			}
			opts := &slog.HandlerOptions{Level: logLevel}
			logger := slog.New(slog.NewJSONHandler(os.Stderr, opts))
			slog.SetDefault(logger)
		},
	}

	rootCmd.PersistentFlags().StringVar(&opts.home, "home", "", "agentd home directory (defaults to AGENTD_HOME or ~/.agentd)")
	rootCmd.PersistentFlags().StringVar(&opts.configFile, "config", "", "path to agentd config file (values override env)")
	rootCmd.PersistentFlags().BoolVarP(&opts.verbose, "verbose", "v", false, "enable verbose output")
	rootCmd.AddCommand(newInitCommand(opts))
	rootCmd.AddCommand(newStartCommand(opts))
	rootCmd.AddCommand(newStatusCommand(opts))
	rootCmd.AddCommand(newCommentCommand(opts))
	rootCmd.AddCommand(newConfigCommand(opts))
	rootCmd.AddCommand(newProjectCommand(opts))
	rootCmd.AddCommand(newSuggestCommand(opts))
	rootCmd.AddCommand(newAskCommand(opts))

	return rootCmd
}
