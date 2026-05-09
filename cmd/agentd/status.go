package main

import (
	"context"

	"agentd/internal/models"

	"github.com/spf13/cobra"
)

func newStatusCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print queue status",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, _, cleanup, err := openRuntime(opts)
			if err != nil {
				return err
			}
			defer cleanup()
			counts, err := taskStateCounts(cmd.Context(), store)
			if err != nil {
				return err
			}
			return printStatus(cmd, counts)
		},
	}
}

func taskStateCounts(ctx context.Context, store models.KanbanStore) (map[models.TaskState]int, error) {
	projects, err := store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[models.TaskState]int)
	for _, project := range projects {
		tasks, err := store.ListTasksByProject(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			counts[task.State]++
		}
	}
	return counts, nil
}

func printStatus(cmd *cobra.Command, counts map[models.TaskState]int) error {
	if err := writeLine(cmd.OutOrStdout(), "STATE             COUNT"); err != nil {
		return err
	}
	for _, state := range statusStates() {
		if err := writeFormat(cmd.OutOrStdout(), "%-17s %d\n", state, counts[state]); err != nil {
			return err
		}
	}
	return writeFormat(cmd.OutOrStdout(), "queue_length      %d\nactive_threads    %d\n",
		counts[models.TaskStateReady]+counts[models.TaskStateQueued], counts[models.TaskStateRunning])
}

func statusStates() []models.TaskState {
	return []models.TaskState{
		models.TaskStateReady, models.TaskStateQueued, models.TaskStateRunning,
		models.TaskStatePending, models.TaskStateCompleted, models.TaskStateFailed,
		models.TaskStateFailedRequiresHuman, models.TaskStateInConsideration,
	}
}
