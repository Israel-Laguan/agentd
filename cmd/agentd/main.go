package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

func execute(ctx context.Context, args []string) error {
	root := newRootCommand()
	if len(args) > 1 {
		root.SetArgs(args[1:])
	}
	return root.ExecuteContext(ctx)
}

func main() {
	ctx := context.Background()
	if err := execute(ctx, os.Args); err != nil {
		slog.Error("command failed", "error", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
