package main

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"agentd/internal/config"
	"agentd/internal/kanban"
)

func newInitCommand(opts *rootOptions) *cobra.Command {
	var resetProfiles bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the local agentd home and database",
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Debug("loading configuration")
			cfg, err := config.Load(config.LoadOptions{
				HomeOverride: opts.home,
				ConfigFile:   opts.configFile,
			})
			if err != nil {
				return fmt.Errorf("load configuration: %w", err)
			}
			if opts.verbose {
				if err := writeConfig(cmd.OutOrStdout(), cfg); err != nil {
					return fmt.Errorf("write config output: %w", err)
				}
			}
			slog.Debug("creating directories", "home", cfg.HomeDir)
			if err := config.EnsureDirs(cfg); err != nil {
				return fmt.Errorf("ensure runtime directories: %w", err)
			}
			slog.Debug("creating crontab", "path", cfg.CronPath)
			if err := config.WriteDefaultCron(cfg.CronPath); err != nil {
				return fmt.Errorf("write crontab: %w", err)
			}

			slog.Debug("initializing database", "path", cfg.DBPath)
			store, err := kanban.OpenStore(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer closeStore(store)
			slog.Debug("seeding agent profiles", "reset", resetProfiles)
			if err := seedDefaultAgent(cmd.Context(), store, resetProfiles); err != nil {
				return fmt.Errorf("seed default agent profiles: %w", err)
			}

			return writeFormat(
				cmd.OutOrStdout(),
				"agentd initialized\nhome: %s\ndatabase: %s\nprojects: %s\ncron: %s\n%s",
				cfg.HomeDir,
				cfg.DBPath,
				cfg.ProjectsDir,
				cfg.CronPath,
				initProfileHint(cfg.Gateway),
			)
		},
	}
	cmd.Flags().BoolVar(&resetProfiles, "reset-profiles", false, "Re-seed all agent profiles, overwriting any existing values")
	return cmd
}
