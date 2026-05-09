package main

import (
	"agentd/internal/config"
	"agentd/internal/kanban"

	"github.com/spf13/cobra"
)

func newConfigCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print current persisted settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.LoadOptions{
				HomeOverride: opts.home,
				ConfigFile:   opts.configFile,
			})
			if err != nil {
				return err
			}
			if err := config.EnsureDirs(cfg); err != nil {
				return err
			}

			store, err := kanban.OpenStore(cfg.DBPath)
			if err != nil {
				return err
			}
			defer closeStore(store)

			settings, err := store.ListSettings(cmd.Context())
			if err != nil {
				return err
			}
			if len(settings) == 0 {
				if err := writeLine(cmd.OutOrStdout(), "no settings configured"); err != nil {
					return err
				}
				return writeCronConfig(cmd.OutOrStdout(), cfg)
			}
			for _, setting := range settings {
				if err := writeFormat(cmd.OutOrStdout(), "%s=%s\n", setting.Key, setting.Value); err != nil {
					return err
				}
			}
			return writeCronConfig(cmd.OutOrStdout(), cfg)
		},
	}
}

func writeCronConfig(out interface {
	Write([]byte) (int, error)
}, cfg config.Config) error {
	if err := writeFormat(out, "cron.path=%s\n", cfg.CronPath); err != nil {
		return err
	}
	if err := writeFormat(out, "cron.task-dispatch=@every %s\n", cfg.Cron.TaskDispatch); err != nil {
		return err
	}
	if err := writeFormat(out, "cron.intake=@every %s\n", cfg.Cron.Intake); err != nil {
		return err
	}
	if err := writeFormat(out, "cron.heartbeat=@every %s\n", cfg.Cron.Heartbeat); err != nil {
		return err
	}
	if err := writeFormat(out, "cron.disk-watchdog=%s\n", cfg.Cron.DiskWatchdog.Spec); err != nil {
		return err
	}
	if err := writeFormat(out, "cron.memory-curator=%s\n", cfg.Cron.MemoryCurator.Spec); err != nil {
		return err
	}
	if err := writeFormat(out, "cron.dream=%s\n", cfg.Cron.Dream.Spec); err != nil {
		return err
	}
	return nil
}

func writeConfig(out interface {
	Write([]byte) (int, error)
}, cfg config.Config) error {
	if err := writeFormat(out, "home=%s\n", cfg.HomeDir); err != nil {
		return err
	}
	if err := writeFormat(out, "db_path=%s\n", cfg.DBPath); err != nil {
		return err
	}
	if err := writeFormat(out, "projects_dir=%s\n", cfg.ProjectsDir); err != nil {
		return err
	}
	if err := writeFormat(out, "uploads_dir=%s\n", cfg.UploadsDir); err != nil {
		return err
	}
	if err := writeFormat(out, "archives_dir=%s\n", cfg.ArchivesDir); err != nil {
		return err
	}
	if err := writeFormat(out, "api.address=%s\n", cfg.API.Address); err != nil {
		return err
	}
	if err := writeFormat(out, "api.materialize_token=%s\n", maskToken(cfg.API.MaterializeToken)); err != nil {
		return err
	}
	if err := writeFormat(out, "gateway.truncator.max_input_chars=%d\n", cfg.Gateway.Truncator.MaxInputChars); err != nil {
		return err
	}
	if err := writeFormat(out, "gateway.truncation.max_input_chars=%d\n", cfg.Gateway.Truncation.MaxInputChars); err != nil {
		return err
	}
	if err := writeFormat(out, "gateway.truncation.stash_threshold=%d\n", cfg.Gateway.Truncation.StashThreshold); err != nil {
		return err
	}
	if err := writeFormat(out, "sandbox.wall_timeout=%s\n", cfg.Sandbox.WallTimeout); err != nil {
		return err
	}
	if err := writeFormat(out, "sandbox.env_allowlist=%v\n", cfg.Sandbox.EnvAllowlist); err != nil {
		return err
	}
	if err := writeCronConfig(out, cfg); err != nil {
		return err
	}
	return nil
}

func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-6:]
}
