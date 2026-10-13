package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

func runPostconditionCommand(t *testing.T, store *testutil.FakeKanbanStore, sb sandbox.Executor, task models.Task, project models.Project) (*mockEventSink, models.Task) {
	t.Helper()
	sink := &mockEventSink{}
	w := NewWorker(store, &routingTestGateway{}, sb, nil, sink, WorkerOptions{})
	w.executeLegacyCommand(context.Background(), task, project, models.AgentProfile{}, "sed -i 's/x/y/' main.go", &legacyTaskAudit{})
	after, err := store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return sink, *after
}

// B-039: the S2 sed corrupted main.go, exited 0, and the task COMPLETED.
// Exit 0 must not complete a task whose success-criteria files no longer parse.
func TestExecuteLegacyCommand_BrokenWorkspaceDoesNotComplete(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, project := seedWorkspaceWithMain(t, store, ctx, []string{"main.go parses", "CountVowels wired into CLI"})
	broken := "package main\n\nfunc main() {\n    fmt.Printf(\"Vowel count: %d\\n\", vowelCount)\n}\n}\n"
	sink, after := runPostconditionCommand(t, store, breakingSandbox{breakWith: broken}, task, project)

	if after.State == models.TaskStateCompleted {
		t.Fatal("task COMPLETED after exit-0 corrupted main.go; want failure handling with retry feedback")
	}
	if !sinkHasType(sink, "RETRY") {
		t.Fatalf("sink events = %#v, want RETRY feedback so the model can repair", sink.events)
	}
}

// Control: a healthy workspace still completes on exit 0.
func TestExecuteLegacyCommand_HealthyWorkspaceCompletes(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, project := seedWorkspaceWithMain(t, store, ctx, []string{"main.go parses"})
	sink, after := runPostconditionCommand(t, store, fixedResultSandbox{result: sandbox.Result{Success: true, Stdout: "ok"}}, task, project)

	if after.State != models.TaskStateCompleted {
		t.Fatalf("task state = %s, want COMPLETED for the happy path", after.State)
	}
	_ = sink
}

// A criterion file that is missing or emptied fails the check too.
func TestExecuteLegacyCommand_MissingCriterionFileFails(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, project := seedWorkspaceWithMain(t, store, ctx, []string{"missing.go exists and parses"})
	_, after := runPostconditionCommand(t, store, fixedResultSandbox{result: sandbox.Result{Success: true, Stdout: "ok"}}, task, project)

	if after.State == models.TaskStateCompleted {
		t.Fatal("task COMPLETED with a missing criterion file; want failure handling")
	}
}

// Tasks without success criteria keep the old exit-0 behavior.
func TestExecuteLegacyCommand_NoCriteriaKeepsExitZeroBehavior(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, project := seedWorkspaceWithMain(t, store, ctx, nil)
	broken := "package main\n\nfunc main() {\n"
	_, after := runPostconditionCommand(t, store, breakingSandbox{breakWith: broken}, task, project)

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
