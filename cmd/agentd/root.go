package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"agentd/internal/config"
)

// extraCommands are added by build-tagged files, such as the devenv build's
// fake ACP agent (fake_acp_agent.go). The slice is initialized here so any
// init() appended during package initialization is visible by the time
// newRootCommand runs. It is populated only at init time and is never exposed
// to external input, plugin loading, or configuration, so the loop below
// executes a fixed, compile-time-known set of factories.
var extraCommands []func() *cobra.Command

type rootOptions struct {
	home       string
	configFile string
	verbose    bool
}

func newRootCommand() *cobra.Command {
	opts := &rootOptions{}

	rootCmd := &cobra.Command{
		Use:           "agentd",
		Short:         "Local-first autonomous workforce daemon",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logLevel := slog.LevelInfo
			if opts.verbose {
				logLevel = slog.LevelDebug
			}
			handlerOpts := &slog.HandlerOptions{Level: logLevel}
			logger := slog.New(slog.NewJSONHandler(os.Stderr, handlerOpts))
			slog.SetDefault(logger)
		},
	}

	rootCmd.PersistentFlags().StringVar(&opts.home, "home", "", "agentd home directory (defaults to AGENTD_HOME or ~/.agentd)")
	rootCmd.PersistentFlags().StringVar(&opts.configFile, "config", "", "path to agentd config file (values override env)")
	rootCmd.PersistentFlags().BoolVarP(&opts.verbose, "verbose", "v", false, "enable verbose output")
	rootCmd.AddCommand(newInitCommand(opts))
	rootCmd.AddCommand(newStartCommand(opts))
	rootCmd.AddCommand(newStatusCommand(opts))
	rootCmd.AddCommand(newReportCommand(opts))
	rootCmd.AddCommand(newCommentCommand(opts))
	rootCmd.AddCommand(newConfigCommand(opts))
	rootCmd.AddCommand(newProjectCommand(opts))
	rootCmd.AddCommand(newSuggestCommand(opts))
	rootCmd.AddCommand(newAskCommand(opts))
	rootCmd.AddCommand(newJailForwardCommand())
	for _, extra := range extraCommands {
		rootCmd.AddCommand(extra())
	}

	return rootCmd
}

// reconfigureSlog applies the config-level log_level, with -v always winning.
// Call after config is loaded to pick up log_level from config.yaml.
func reconfigureSlog(verbose bool, cfgLogLevel string) {
	if verbose {
		return // -v already set debug in PersistentPreRun
	}
	level := config.ParseLogLevel(cfgLogLevel)
	handlerOpts := &slog.HandlerOptions{Level: level}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, handlerOpts))
	slog.SetDefault(logger)
}
