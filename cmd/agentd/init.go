package main

import (
	"log/slog"

	"agentd/internal/config"
	"agentd/internal/kanban"

	"github.com/spf13/cobra"
)

func newInitCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the local agentd home and database",
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Debug("loading configuration")
			cfg, err := config.Load(config.LoadOptions{
				HomeOverride: opts.home,
				ConfigFile:   opts.configFile,
			})
			if err != nil {
				return err
			}
			if opts.verbose {
				if err := writeConfig(cmd.OutOrStdout(), cfg); err != nil {
					return err
				}
			}
			slog.Debug("creating directories", "home", cfg.HomeDir)
			if err := config.EnsureDirs(cfg); err != nil {
				return err
			}
			slog.Debug("creating crontab", "path", cfg.CronPath)
			if err := config.WriteDefaultCron(cfg.CronPath); err != nil {
				return err
			}

			slog.Debug("initializing database", "path", cfg.DBPath)
			store, err := kanban.OpenStore(cfg.DBPath)
			if err != nil {
				return err
			}
			defer closeStore(store)
			slog.Debug("seeding agent profiles")
			if err := seedDefaultAgent(cmd.Context(), store); err != nil {
				return err
			}

			return writeFormat(
				cmd.OutOrStdout(),
				"agentd initialized\nhome: %s\ndatabase: %s\nprojects: %s\ncron: %s\n",
				cfg.HomeDir,
				cfg.DBPath,
				cfg.ProjectsDir,
				cfg.CronPath,
			)
		},
	}
}
