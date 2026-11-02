package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
)

// A task whose tool credentials are missing fails at session start, before the
// pre-task clarification can put a question to a human.
func TestAgenticMissingCredentialFailsBeforeClarification(t *testing.T) {
	const envKey = "TEST_SESSION_START_BEFORE_CLARIFY_GH"
	t.Setenv(envKey, "")
	gw := &elicitationSequenceGateway{elicitationJSON: sampleElicitationJSON()}
	opts := WorkerOptions{MaxToolIterations: 5, ToolCredentials: map[string]string{"github": envKey}}
	draft := models.DraftTask{Title: "Fix the bug", Description: "fix it"}
	w, _, task, project, profile := agenticFixtureWith(t, gw, &scriptedSandbox{}, draft, nil, opts)
	ctx := context.Background()

	w.processAgentic(ctx, task, project, profile)

	got, err := w.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailed {
		t.Fatalf("state = %s, want FAILED: the missing credential must stop the task", got.State)
	}
	events, err := w.store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask: %v", err)
	}
	var resultPayload string
	for _, event := range events {
		if event.Type == models.EventTypeResult {
			resultPayload = event.Payload
		}
	}
	if !strings.Contains(resultPayload, "missing credentials") || !strings.Contains(resultPayload, envKey) {
		t.Fatalf("failure payload = %q, want it to name the missing credential %s", resultPayload, envKey)
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	if len(children) != 0 || gw.elicitationCalls != 0 || gw.agenticCalls != 0 {
		t.Fatalf("work ran after a failed session start: subtasks=%d elicitation calls=%d agentic calls=%d", len(children), gw.elicitationCalls, gw.agenticCalls)
	}
}
