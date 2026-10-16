package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

const hostReportFixture = "Operating System: Debian GNU/Linux 13 (trixie)\n  Machine ID: 9ebbf53566051e82c3e15b536a381807\n          Kernel: Linux 7.2.9-2-liquorix-amd64\n"

func seedHostReportTask(t *testing.T, store *testutil.FakeKanbanStore, description string, files map[string]string) (models.Task, models.Project) {
	t.Helper()
	ctx := context.Background()
	store.SetProjectsDir(t.TempDir())
	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "host", Description: "d",
		Tasks: []models.DraftTask{{Title: "Build report", Description: description}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := os.MkdirAll(proj.WorkspacePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(proj.WorkspacePath, name), []byte(content), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	return tasks[0], *proj
}

// panicSandbox fails the test if the worker executes anything.
type panicSandbox struct{ t *testing.T }

func (s panicSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	s.t.Helper()
	s.t.Error("sandbox executed; the report must be rendered by code, not a shell command")
	return sandbox.Result{}, nil
}

// B-045: the report is a summary rendered from the collected files, never the
// model's own text or a cat of raw output, and it never carries identifiers.
func TestRunLegacyTaskRendersHostReportWithoutModelOrShell(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	files := map[string]string{
		"os_info.txt":   hostReportFixture,
		"cpu_info.txt":  "Architecture:                            x86_64\nModel name:                              Intel(R) Core(TM) Ultra 5 226V\nCPU(s):                                  8\n",
		"mem_info.txt":  "               total        used        free\nMem:            15Gi       7.9Gi       1.2Gi\n",
		"disk_info.txt": "NAME        MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS\nnvme0n1     259:0    0 476.9G  0 disk\n",
	}
	task, project := seedHostReportTask(t, store, "Create system report.md summarizing the OS and hardware from the collected files", files)
	gw := &routingTestGateway{}
	sink := &mockEventSink{}
	w := NewWorker(store, gw, panicSandbox{t}, nil, sink, WorkerOptions{})
	w.RunLegacyTask(ctx, task, project, models.AgentProfile{}, true)

	if len(gw.requests) != 0 {
		t.Fatalf("gateway called %d times, want 0", len(gw.requests))
	}
	got, err := os.ReadFile(filepath.Join(project.WorkspacePath, "report.md"))
	if err != nil {
		t.Fatalf("report.md not written: %v", err)
	}
	for _, want := range []string{"Debian GNU/Linux 13 (trixie)", "Linux 7.2.9-2-liquorix-amd64"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("report lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "9ebbf535") || strings.Contains(string(got), "Machine ID") {
		t.Fatalf("report leaks the machine id:\n%s", got)
	}
	if after, _ := store.GetTask(ctx, task.ID); after.State != models.TaskStateCompleted {
		t.Fatalf("task state = %s, want COMPLETED", after.State)
	}
}

// B-046: a report missing a core fact must not burn retries re-rendering the
// same workspace (the collectors already ran, so the next attempt would hit
// the same gap). The renderer steps aside and the normal model path runs.
func TestRunLegacyTaskFallsBackToModelOnPartialReport(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	files := map[string]string{
		"os_info.txt": hostReportFixture,
		"mem.txt":     "               total        used        free\nMem:            15Gi       7.9Gi       1.2Gi\n",
	}
	task, project := seedHostReportTask(t, store, "Create system report.md summarizing the OS and hardware from the collected files", files)
	gw := &routingTestGateway{}
	w := NewWorker(store, gw, fixedResultSandbox{result: sandbox.Result{Success: true}}, nil, &mockEventSink{}, WorkerOptions{})
	w.RunLegacyTask(ctx, task, project, models.AgentProfile{}, true)

	if len(gw.requests) == 0 {
		t.Fatal("gateway never called, want the model path for partial facts")
	}
	if _, err := os.Stat(filepath.Join(project.WorkspacePath, "report.md")); err == nil {
		t.Fatal("report.md was written for partial facts; the model path owns the task instead")
	}
	if after, _ := store.GetTask(ctx, task.ID); after.State != models.TaskStateCompleted {
		t.Fatalf("task state = %s, want COMPLETED via the model path", after.State)
	}
}

// With nothing collected the renderer steps aside and the normal model path runs.
func TestRunLegacyTaskFallsBackToModelWhenNoHostFactsCollected(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, project := seedHostReportTask(t, store, "Create system report.md summarizing the OS and hardware", map[string]string{"notes.txt": "nothing"})
	gw := &routingTestGateway{}
	w := NewWorker(store, gw, fixedResultSandbox{result: sandbox.Result{Success: true}}, nil, &mockEventSink{}, WorkerOptions{})
	w.RunLegacyTask(ctx, task, project, models.AgentProfile{}, true)
	if len(gw.requests) == 0 {
		t.Fatal("gateway never called, want the model path when no facts were collected")
	}
}
