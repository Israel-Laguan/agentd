package main

import (
	"fmt"
	"log/slog"

	"agentd/internal/config"
	"agentd/internal/kanban"
	"agentd/internal/models"

	"github.com/spf13/cobra"
)

type projectOptions struct {
	name        string
	description string
}

func newProjectCommand(opts *rootOptions) *cobra.Command {
	projectOpts := &projectOptions{}
	cmd := &cobra.Command{Use: "project", Short: "Manage projects"}
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a project and workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectCreate(cmd, opts, projectOpts)
		},
	}
	create.Flags().StringVar(&projectOpts.name, "name", "", "project name")
	create.Flags().StringVar(&projectOpts.description, "description", "", "project description")
	cmd.AddCommand(create)
	return cmd
}

func runProjectCreate(cmd *cobra.Command, opts *rootOptions, p *projectOptions) error {
	_, _, deps, cleanup, err := openRuntime(opts)
	if err != nil {
		return err
	}
	defer cleanup()
	project, tasks, err := deps.project.MaterializePlan(cmd.Context(), projectPlan(p))
	if err != nil {
		return err
	}
	return writeFormat(cmd.OutOrStdout(), "project_id=%s\nworkspace=%s\ntask_id=%s\n", project.ID, project.WorkspacePath, tasks[0].ID)
}

func projectPlan(p *projectOptions) models.DraftPlan {
	return models.DraftPlan{
		ProjectName: p.name,
		Description: p.description,
		Tasks: []models.DraftTask{{
			TempID: "task-1", Title: p.name, Description: p.description,
			Assignee: models.TaskAssigneeSystem,
		}},
	}
}

func openRuntime(opts *rootOptions) (config.Config, *kanban.Store, runtimeDeps, func(), error) {
	cfg, err := config.Load(config.LoadOptions{
		HomeOverride: opts.home,
		ConfigFile:   opts.configFile,
	})
	if err != nil {
		return config.Config{}, nil, runtimeDeps{}, nil, err
	}
	if err := config.EnsureDirs(cfg); err != nil {
		return config.Config{}, nil, runtimeDeps{}, nil, err
	}

	checkResult := config.CheckProviders(cfg.Gateway)
	if !checkResult.Available {
		if checkResult.HordeAvailable {
			slog.Warn("No LLM API keys configured and local provider not available. Falling back to AI Horde (anonymous, async, not recommended for production use)")
		} else {
			return config.Config{}, nil, runtimeDeps{}, nil, fmt.Errorf("no LLM providers available. Configure OPENAI_API_KEY, ANTHROPIC_API_KEY, or set up a local OpenAI-compatible endpoint")
		}
	}

	store, err := kanban.OpenStore(cfg.DBPath)
	if err != nil {
		return config.Config{}, nil, runtimeDeps{}, nil, err
	}
	return cfg, store, newRuntimeDeps(cfg, store), func() { closeStore(store) }, nil
}
