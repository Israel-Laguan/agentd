package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

const validMainGo = "package main\n\nfunc main() {}\n"

// breakingSandbox replays the S2 B-039 scenario: it corrupts main.go in the
// workspace (like the stray-`sed} that split a string and left a stray
// brace) yet reports exit 0.
type breakingSandbox struct {
	breakWith string
}

func (s breakingSandbox) Execute(_ context.Context, p sandbox.Payload) (sandbox.Result, error) {
	if err := os.WriteFile(filepath.Join(p.WorkspacePath, "main.go"), []byte(s.breakWith), 0o644); err != nil {
		return sandbox.Result{}, err
	}
	return sandbox.Result{Success: true, ExitCode: 0, Stdout: "patched"}, nil
}

func seedWorkspaceWithMain(t *testing.T, store *testutil.FakeKanbanStore, ctx context.Context, criteria []string) (models.Task, models.Project) {
	t.Helper()
	store.SetProjectsDir(t.TempDir())
	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "wordcount", Description: "d",
		Tasks: []models.DraftTask{{
			Title:           "Wire CountVowels into CLI",
			Description:     testutil.AgenticTestTaskDescription(),
			SuccessCriteria: criteria,
		}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := os.MkdirAll(proj.WorkspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(proj.WorkspacePath, "main.go"), []byte(validMainGo), 0o644); err != nil {
		t.Fatalf("seed main.go: %v", err)
	}
	return tasks[0], *proj
}

// runPostconditionAgentic runs one agentic turn that edits main.go with sed,
// then answers with text. The post-condition gate runs before that answer
// commits, so a criterion file that no longer parses keeps the task from
// completing.
func runPostconditionAgentic(t *testing.T, store *testutil.FakeKanbanStore, sb sandbox.Executor, task models.Task) (*mockEventSink, models.Task) {
	t.Helper()
	ctx := context.Background()
	if err := store.UpsertAgentProfile(ctx, models.AgentProfile{ID: task.AgentID, Provider: "openai", Model: "gpt-4", AgenticMode: true}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	sink := &mockEventSink{}
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{ToolCalls: []gateway.ToolCall{testutil.BashToolCall("call_1", "sed -i 's/x/y/' main.go")}},
		{Content: "done"},
	}}
	w := NewWorker(store, gw, sb, nil, sink, WorkerOptions{})
	w.Process(ctx, task)
	after, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return sink, *after
}

// B-039: the S2 sed corrupted main.go, exited 0, and the task COMPLETED.
// Exit 0 must not complete a task whose success-criteria files no longer parse.
// On the agentic path the finish is unverified, so it goes to a human.
func TestPostconditionBrokenWorkspaceHandsTaskToHuman(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, _ := seedWorkspaceWithMain(t, store, ctx, []string{"main.go parses", "CountVowels wired into CLI"})
	broken := "package main\n\nfunc main() {\n    fmt.Printf(\"Vowel count: %d\\n\", vowelCount)\n}\n}\n"
	_, after := runPostconditionAgentic(t, store, breakingSandbox{breakWith: broken}, task)

	if after.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("task state = %s after exit-0 corrupted main.go, want FAILED_REQUIRES_HUMAN", after.State)
	}
}

// Control: a healthy workspace still completes on exit 0.
func TestPostconditionHealthyWorkspaceCompletes(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, _ := seedWorkspaceWithMain(t, store, ctx, []string{"main.go parses"})
	sink, after := runPostconditionAgentic(t, store, fixedResultSandbox{result: sandbox.Result{Success: true, Stdout: "ok"}}, task)

	if after.State != models.TaskStateCompleted {
		t.Fatalf("task state = %s, want COMPLETED for the happy path", after.State)
	}
	_ = sink
}

// A criterion file that is missing or emptied fails the check too.
func TestPostconditionMissingCriterionFileFails(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, _ := seedWorkspaceWithMain(t, store, ctx, []string{"missing.go exists and parses"})
	_, after := runPostconditionAgentic(t, store, fixedResultSandbox{result: sandbox.Result{Success: true, Stdout: "ok"}}, task)

	if after.State == models.TaskStateCompleted {
		t.Fatal("task COMPLETED with a missing criterion file; want failure handling")
	}
}

// Tasks without success criteria keep the old exit-0 behavior.
func TestPostconditionNoCriteriaKeepsExitZeroBehavior(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, _ := seedWorkspaceWithMain(t, store, ctx, nil)
	broken := "package main\n\nfunc main() {\n"
	_, after := runPostconditionAgentic(t, store, breakingSandbox{breakWith: broken}, task)

	if after.State != models.TaskStateCompleted {
		t.Fatalf("task state = %s, want COMPLETED when no criteria constrain it", after.State)
	}
}

func TestCriterionFiles_Extraction(t *testing.T) {
	tests := []struct {
		name     string
		criteria []string
		want     []string
	}{
		{name: "bare go file", criteria: []string{"main.go parses"}, want: []string{"main.go"}},
		{name: "nested path", criteria: []string{"wire up internal/api/server.go"}, want: []string{"internal/api/server.go"}},
		{name: "prose without files", criteria: []string{"CLI prints friendly output"}, want: nil},
		{name: "url ignored", criteria: []string{"see https://example.com/docs for API"}, want: nil},
		{name: "duplicates collapse", criteria: []string{"main.go builds", "main.go parses"}, want: []string{"main.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := criterionFiles(tt.criteria)
			if len(got) != len(tt.want) {
				t.Fatalf("criterionFiles = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("criterionFiles = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}
