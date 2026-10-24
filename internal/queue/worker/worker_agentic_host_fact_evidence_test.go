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

const (
	chainedRedirect = "uname -s && uname -r > os_info.txt"
	groupedRedirect = "{ uname -s; uname -r; } > os_info.txt"
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

// runAgenticHostFactTask runs one agentic attempt of a host-fact collector task
// through the real filter-only executor and returns the task afterwards.
func runAgenticHostFactTask(t *testing.T, gw gateway.AIGateway) (*testutil.FakeKanbanStore, models.Task, string) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	root := t.TempDir()
	store.SetProjectsDir(root)
	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "host", Description: "d",
		Tasks: []models.DraftTask{{Title: "Collect OS information", Description: "Run uname -s and uname -r and save their combined output to os_info.txt.\n" + testutil.AgenticTestTaskDescription()}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := os.MkdirAll(proj.WorkspacePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	profile := models.AgentProfile{ID: tasks[0].AgentID, Provider: "openai", Model: "gpt-4", AgenticMode: true}
	if err := store.UpsertAgentProfile(ctx, profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	sink := &storeEventSink{store: store}
	sb := &sandbox.BashExecutor{Root: root, Sink: sink, Inactivity: 5 * time.Second, Backend: sandbox.BackendFilterOnly}
	w := NewWorker(store, gw, sb, nil, sink, WorkerOptions{MaxToolIterations: 5})
	if result, ok := w.processAgentic(ctx, *running, *proj, profile); ok {
		w.handleLoopResult(ctx, *running, result)
	}
	after, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return store, *after, proj.WorkspacePath
}

// B-046/B-047 on the agentic path (T-055b gap 4): the host-fact evidence guard
// needs the task's intent on the bash payload. A chain whose redirect binds
// only its last command is refused with grouping feedback the model sees in
// the same loop; the grouped rerun writes both outputs and the task completes.
func TestAgenticChainedRedirectRefusedThenGroupedRerunSucceeds(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", chainedRedirect), bashCall("c2", groupedRedirect), {Content: "Saved the OS information."},
	}}
	store, after, workspace := runAgenticHostFactTask(t, gw)

	violations := eventsOfType(t, store, after.ID, models.EventType("SANDBOX_VIOLATION"))
	if len(violations) != 1 || !strings.Contains(violations[0], "group the commands") || !strings.Contains(violations[0], chainedRedirect) {
		t.Fatalf("SANDBOX_VIOLATION = %#v, want one refusal naming the command and the grouping fix", violations)
	}
	if gw.callCount != 3 || !requestContainsToolResult(gw.requests[1], "c1", "group the commands") {
		t.Fatalf("gateway calls = %d; the model must see the grouping feedback for the refused call", gw.callCount)
	}
	if after.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED after the grouped rerun", after.State)
	}
	out, err := os.ReadFile(filepath.Join(workspace, "os_info.txt"))
	if err != nil {
		t.Fatalf("read os_info.txt: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(out)), "\n"); len(lines) != 2 {
		t.Fatalf("os_info.txt = %q, want the output of both commands", out)
	}
}

// Host facts typed from the model's memory are refused on the agentic path:
// the literal write never runs, so no file holds invented facts.
func TestAgenticLiteralHostFactsRefused(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "echo Linux 6.1 > os_info.txt"), {Content: "Saved the OS information."},
	}}
	store, after, workspace := runAgenticHostFactTask(t, gw)

	violations := eventsOfType(t, store, after.ID, models.EventType("SANDBOX_VIOLATION"))
	if len(violations) != 1 || !strings.Contains(violations[0], "not literal text") {
		t.Fatalf("SANDBOX_VIOLATION = %#v, want one literal-text refusal", violations)
	}
	if _, err := os.Stat(filepath.Join(workspace, "os_info.txt")); err == nil {
		t.Fatal("os_info.txt exists; the literal write must not run")
	}
	if after.State == models.TaskStateCompleted {
		t.Fatal("task completed after its only write was refused")
	}
}

// A delegated child's shell commands carry the parent task's intent, so the
// evidence guard holds for the whole delegation tree.
func TestDelegatedBashCarriesParentTaskIntent(t *testing.T) {
	t.Parallel()
	gw := &delegatingGateway{toolName: "delegate", delegateArgs: `{"subagent":"inspector","task":"uname"}`}
	sb := &payloadCapturingSandbox{}
	runAgenticTaskIn(t, gw, sb, seedSubagent)

	if len(sb.payloads) != 1 || !strings.Contains(sb.payloads[0].Intent, "Inspect host") {
		t.Fatalf("sandbox payloads = %+v, want the child's bash payload to carry the parent task's intent", sb.payloads)
	}
}
