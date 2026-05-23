package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func execute(ctx context.Context, args []string) error {
	root := newRootCommand()
	if len(args) > 1 {
		root.SetArgs(args[1:])
	}
	return root.ExecuteContext(ctx)
}

func reportCobraUsageError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unknown command"),
		strings.Contains(msg, "unknown flag"),
		strings.Contains(msg, "required flag"),
		strings.Contains(msg, "invalid argument"),
		strings.Contains(msg, "flag needs an argument"),
		strings.Contains(msg, "accepts "):
		fmt.Fprintln(os.Stderr, msg)
		root := newRootCommand()
		fmt.Fprintln(os.Stderr)
		_ = root.Usage()
		return true
	default:
		return false
	}
}

func main() {
	ctx := context.Background()
	if err := execute(ctx, os.Args); err != nil {
		if reportCobraUsageError(err) {
			os.Exit(1)
		}
		reportCommandError(err)
		os.Exit(1)
	}
}
