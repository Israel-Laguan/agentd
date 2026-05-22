package main

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/kanban"
)

func TestCommentCommand_AddsComment(t *testing.T) {
	home := initHome(t)
	createOut := runCLI(t, home, "project", "create", "--name", "comment-test", "--description", "desc")
	taskID := outputValue(t, createOut, "task_id")

	output := runCLI(t, home, "comment", taskID, "looks good")
	if !strings.Contains(output, "comment added") {
		t.Fatalf("unexpected output: %s", output)
	}

	cfg, err := config.Load(config.LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := kanban.OpenStore(cfg.DBPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer closeStore(store)

	comments, err := store.ListComments(context.Background(), taskID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "looks good" {
		t.Fatalf("comments = %+v, want one with body looks good", comments)
	}
}
