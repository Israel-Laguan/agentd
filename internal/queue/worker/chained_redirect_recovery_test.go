package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

func eventsOfType(t *testing.T, store *testutil.FakeKanbanStore, taskID string, typ models.EventType) []string {
	t.Helper()
	events, err := store.ListEventsByTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("ListEventsByTask: %v", err)
	}
	var payloads []string
	for _, ev := range events {
		if ev.Type == typ {
			payloads = append(payloads, ev.Payload)
		}
	}
	return payloads
}

// runLegacyAttempt claims the task and runs one legacy attempt, as the
// dispatcher does for each try.
func runLegacyAttempt(t *testing.T, w *Worker, store *testutil.FakeKanbanStore, taskID string, project models.Project) models.Task {
	t.Helper()
	ctx := context.Background()
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning (state %s): %v", task.State, err)
	}
	w.RunLegacyTask(ctx, *running, project, models.AgentProfile{}, true)
	after, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return *after
}

const (
	chainedRedirect = "uname -s && uname -r > os_info.txt"
	groupedRedirect = "{ uname -s; uname -r; } > os_info.txt"
)

// seedHostFactTask materializes one host-fact collector task with a real
// workspace under a fresh projects root.
func seedHostFactTask(t *testing.T, store *testutil.FakeKanbanStore) (string, string, models.Project) {
	t.Helper()
	root := t.TempDir()
	store.SetProjectsDir(root)
	proj, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{ProjectName: "host", Description: "d",
		Tasks: []models.DraftTask{{Title: "Collect OS information", Description: "Run uname -s and uname -r and save their combined output to os_info.txt"}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := os.MkdirAll(proj.WorkspacePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return root, tasks[0].ID, *proj
}

func assertChainedRefused(t *testing.T, store *testutil.FakeKanbanStore, after models.Task, workspace string) {
	t.Helper()
	if after.State == models.TaskStateCompleted || after.State == models.TaskStateFailed {
		t.Fatalf("after the chained command: state = %s, want a retry", after.State)
	}
	violations := eventsOfType(t, store, after.ID, models.EventType("SANDBOX_VIOLATION"))
	if len(violations) != 1 || !strings.Contains(violations[0], "group the commands") || !strings.Contains(violations[0], chainedRedirect) {
		t.Fatalf("SANDBOX_VIOLATION = %#v, want one refusal naming the command and the grouping fix", violations)
	}
	if _, err := os.Stat(filepath.Join(workspace, "os_info.txt")); err == nil {
		t.Fatal("os_info.txt exists after the refusal; the chained command must not run")
	}
}

func assertGroupedRetrySucceeded(t *testing.T, store *testutil.FakeKanbanStore, gw *sequenceGateway, after models.Task, workspace string) {
	t.Helper()
	if after.State != models.TaskStateCompleted {
		t.Fatalf("after the grouped retry: state = %s, want COMPLETED", after.State)
	}
	if gw.callCount != 2 || !promptContains(gw.requests[1], "group the commands") {
		t.Fatalf("gateway calls = %d; the retry prompt must carry the grouping feedback", gw.callCount)
	}
	if got := eventsOfType(t, store, after.ID, models.EventTypeCommand); len(got) != 1 || got[0] != groupedRedirect {
		t.Fatalf("COMMAND events = %#v, want only the grouped command (the refused one never ran)", got)
	}
	out, err := os.ReadFile(filepath.Join(workspace, "os_info.txt"))
	if err != nil {
		t.Fatalf("read os_info.txt: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(out)), "\n"); len(lines) != 2 {
		t.Fatalf("os_info.txt = %q, want the output of both commands", out)
	}
}

// B-047: the B-046 chained-redirect guard end to end, through the real
// filter-only executor and a scripted model. The model first sends a chain
// whose redirect binds only to its last command; the sandbox refuses it with
// grouping feedback, the retry prompt carries that feedback, and the grouped
// command runs and writes both outputs to the file.
func TestChainedRedirectRefusedThenGroupedRetrySucceeds(t *testing.T) {
	store := testutil.NewFakeStore()
	root, taskID, project := seedHostFactTask(t, store)
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: `{"command":"` + chainedRedirect + `"}`},
		{Content: `{"command":"` + groupedRedirect + `"}`},
	}}
	// Both the worker and the executor write into the store, as the daemon's bus
	// does, so retry feedback built from stored SANDBOX_VIOLATION events sees them.
	sink := &storeEventSink{store: store}
	sb := &sandbox.BashExecutor{Root: root, Sink: sink, Inactivity: 5 * time.Second, Backend: sandbox.BackendFilterOnly}
	w := NewWorker(store, gw, sb, nil, sink, WorkerOptions{})

	assertChainedRefused(t, store, runLegacyAttempt(t, w, store, taskID, project), project.WorkspacePath)
	assertGroupedRetrySucceeded(t, store, gw, runLegacyAttempt(t, w, store, taskID, project), project.WorkspacePath)
}

func promptContains(req gateway.AIRequest, want string) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, want) {
			return true
		}
	}
	return false
}
