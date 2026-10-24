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

const sandboxFeedbackPrefix = "The sandbox refused your earlier command(s):"

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

// sandboxFeedbackMessage returns the sandbox-refusal user message a request
// carries, or "" when it carries none.
func sandboxFeedbackMessage(req gateway.AIRequest) string {
	for _, m := range req.Messages {
		if m.Role == "user" && strings.HasPrefix(m.Content, sandboxFeedbackPrefix) {
			return m.Content
		}
	}
	return ""
}

// runAgenticWithViolations runs one agentic attempt on a task that already has
// the given SANDBOX_VIOLATION payloads, and returns the first model request.
func runAgenticWithViolations(t *testing.T, payloads ...string) gateway.AIRequest {
	t.Helper()
	ctx := context.Background()
	gw := &sequenceGateway{responses: []gateway.AIResponse{{Content: "Done."}}}
	draft := models.DraftTask{Title: "Inspect host", Description: testutil.AgenticTestTaskDescription()}
	w, _, task, project, profile := agenticFixture(t, gw, nil, draft, nil)
	for _, payload := range payloads {
		addViolationEvent(t, w.store, task, payload)
	}
	if result, ok := w.processAgentic(ctx, task, project, profile); ok {
		w.handleLoopResult(ctx, task, result)
	}
	if len(gw.requests) == 0 {
		t.Fatal("no model call made")
	}
	return gw.requests[0]
}

// T-055b gap 1: the agentic retry is told which earlier command the sandbox
// refused, as the legacy command prompt was. A repeat of the same refusal is
// listed once, and the guidance names the workspace rule and host-fact commands.
func TestAgenticRetryIncludesSandboxViolationFeedback(t *testing.T) {
	t.Parallel()
	reason := "sandbox path violation: absolute path escapes workspace: /etc/os-release; command: cat /etc/os-release"

	feedback := sandboxFeedbackMessage(runAgenticWithViolations(t, reason, reason))
	if feedback == "" {
		t.Fatal("first request carries no sandbox refusal feedback")
	}
	if strings.Count(feedback, "/etc/os-release") != 2 { // once in the reason, once in the command
		t.Fatalf("feedback = %q, want the duplicate refusal listed once", feedback)
	}
	if !strings.Contains(feedback, "inside the project workspace") {
		t.Fatalf("feedback = %q, want guidance to stay inside the workspace", feedback)
	}
	for _, want := range []string{"hostnamectl", "lscpu", "uname -a"} {
		if !strings.Contains(feedback, want) {
			t.Fatalf("feedback = %q, want host-introspection hint %q", feedback, want)
		}
	}
}

func TestAgenticRunWithoutViolationsSendsNoSandboxFeedback(t *testing.T) {
	t.Parallel()
	if feedback := sandboxFeedbackMessage(runAgenticWithViolations(t)); feedback != "" {
		t.Fatalf("request carries sandbox feedback with no refusals: %q", feedback)
	}
}
