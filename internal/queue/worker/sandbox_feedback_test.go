package worker

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func addViolationEvent(t *testing.T, store models.KanbanStore, task models.Task, payload string) {
	t.Helper()
	err := store.AppendEvent(context.Background(), models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      "SANDBOX_VIOLATION",
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}
}

func TestPrependSandboxViolationFeedback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "sandbox-feedback", "inspect host")
	w := &Worker{store: store}
	base := []gateway.PromptMessage{{Role: "system", Content: "sys"}, {Role: "user", Content: "task"}}

	if got := w.PrependSandboxViolationFeedback(ctx, task, base); len(got) != len(base) {
		t.Fatalf("no violations: messages = %d, want %d", len(got), len(base))
	}

	reason := "sandbox path violation: absolute path escapes workspace: /etc/os-release; command: cat /etc/os-release"
	addViolationEvent(t, store, task, reason)
	addViolationEvent(t, store, task, reason) // duplicate must not repeat
	got := w.PrependSandboxViolationFeedback(ctx, task, base)
	if len(got) != len(base)+1 {
		t.Fatalf("messages = %d, want %d", len(got), len(base)+1)
	}
	last := got[len(got)-1]
	if last.Role != "user" {
		t.Fatalf("feedback role = %q, want user", last.Role)
	}
	if strings.Count(last.Content, "/etc/os-release") != 2 { // once in reason token, once in command
		t.Fatalf("feedback = %q, want duplicate violation listed once", last.Content)
	}
	if !strings.Contains(last.Content, "inside the project workspace") {
		t.Fatalf("feedback = %q, want guidance to stay inside the workspace", last.Content)
	}
	for _, want := range []string{"hostnamectl", "lscpu", "uname -a"} {
		if !strings.Contains(last.Content, want) {
			t.Fatalf("feedback = %q, want host-introspection hint %q", last.Content, want)
		}
	}
}

func TestCommandIncludesSandboxViolationFeedbackOnRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "sandbox-retry", "inspect host")
	addViolationEvent(t, store, task, "sandbox path violation: absolute path escapes workspace: /proc/cpuinfo; command: cat /proc/cpuinfo")
	gw := &sequenceGateway{responses: []gateway.AIResponse{{Content: `{"command":"uname -a"}`}}}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{})
	project, err := store.GetProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}

	if _, _, _, err := w.command(ctx, task, *project, models.AgentProfile{}); err != nil {
		t.Fatalf("command() error = %v", err)
	}
	msgs := gw.requests[0].Messages
	if last := msgs[len(msgs)-1].Content; !strings.Contains(last, "/proc/cpuinfo") {
		t.Fatalf("last message = %q, want sandbox violation fed back", last)
	}
}
