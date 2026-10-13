package worker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestEvict_PostsHumanReadableFailureComment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "evict-comment", "inspect host")
	w := &Worker{store: store, sink: &mockEventSink{}, maxRetries: 3}
	task.RetryCount = 3

	w.evict(ctx, task, "sandbox path violation: absolute path escapes workspace: /etc/os-release")

	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(comments))
	}
	c := comments[0]
	if c.Author != models.CommentAuthorWorkerAgent {
		t.Fatalf("author = %q, want %q", c.Author, models.CommentAuthorWorkerAgent)
	}
	for _, want := range []string{"/etc/os-release", "3", "/retry"} {
		if !strings.Contains(c.Body, want) {
			t.Fatalf("comment = %q, want substring %q", c.Body, want)
		}
	}
}

func TestFailTerminal_PostsFailureComment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "terminal-comment", "inspect host")
	w := &Worker{store: store, sink: &mockEventSink{}}

	w.failTerminal(ctx, task, errString("max_healing_tasks cap reached: x"), models.TaskStateFailed)

	comments, _ := store.ListComments(ctx, task.ID)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "max_healing_tasks cap reached") {
		t.Fatalf("comments = %#v, want one naming the failure", comments)
	}
	if !strings.Contains(comments[0].Body, fmt.Sprintf("%d", task.RetryCount)) {
		t.Fatalf("comments = %#v, want retry count %d", comments, task.RetryCount)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestWarnIfWorkspaceEmpty_SuppressedWhenStartedEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		startedEmpty bool
		wantWarning  bool
	}{
		{"started empty is an explicit choice", true, false},
		{"unseeded empty workspace still warns", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sink := &mockEventSink{}
			w := &Worker{sink: sink}
			project := &models.Project{WorkspacePath: t.TempDir(), StartedEmpty: tt.startedEmpty}
			if _, err := os.ReadDir(project.WorkspacePath); err != nil {
				t.Fatal(err)
			}

			w.warnIfWorkspaceEmpty(context.Background(), models.Task{ProjectID: "p", BaseEntity: models.BaseEntity{ID: "t"}}, project)

			got := false
			for _, ev := range sink.events {
				if ev.Type == models.EventTypeWarning {
					got = true
				}
			}
			if got != tt.wantWarning {
				t.Fatalf("warning emitted = %v, want %v", got, tt.wantWarning)
			}
		})
	}
}
