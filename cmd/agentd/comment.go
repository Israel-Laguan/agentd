package main

import (
	"agentd/internal/models"

	"github.com/spf13/cobra"
)

func newCommentCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "comment <task-id> <message>",
		Short: "Add a human comment to a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, _, cleanup, err := openRuntime(opts)
			if err != nil {
				return err
			}
			defer cleanup()
			err = store.AddComment(cmd.Context(), models.Comment{
				TaskID: args[0],
				Author: "Human",
				Body:   args[1],
			})
			if err != nil {
				return err
			}
			return writeLine(cmd.OutOrStdout(), "comment added")
		},
	}
}
