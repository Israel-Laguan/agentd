package main

import (
	"context"
	"testing"
)

func TestExecuteHelpReturnsNil(t *testing.T) {
	ctx := context.Background()
	if err := execute(ctx, []string{"agentd", "--help"}); err != nil {
		t.Fatalf("execute help: %v", err)
	}
}

func TestExecuteUnknownCommandErrors(t *testing.T) {
	ctx := context.Background()
	err := execute(ctx, []string{"agentd", "definitely-not-a-real-subcommand"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}
