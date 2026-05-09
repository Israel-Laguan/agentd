package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"agentd/internal/kanban"
	"agentd/internal/models"

	"github.com/spf13/cobra"
)

func TestStatusPrintsTaskStateCounts(t *testing.T) {
	store := openInMemoryStore(t)
	ctx := context.Background()

	seedStatusPlan(t, ctx, store)
	counts, err := taskStateCounts(ctx, store)
	if err != nil {
		t.Fatalf("taskStateCounts() error = %v", err)
	}
	assertStateCounts(t, counts)
	assertStatusOutput(t, counts)
}

func seedStatusPlan(t *testing.T, ctx context.Context, store *kanban.Store) {
	t.Helper()
	plan := models.DraftPlan{
		ProjectName: "status plan",
		Description: "test counts",
		Tasks: []models.DraftTask{
			{TempID: "a", Title: "A"},
			{TempID: "b", Title: "B"},
			{TempID: "c", Title: "C", DependsOn: []string{"a"}},
			{TempID: "d", Title: "D"},
			{TempID: "e", Title: "E"},
		},
	}
	_, tasks, err := store.MaterializePlan(ctx, plan)
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}

	byTitle := make(map[string]models.Task, len(tasks))
	for _, task := range tasks {
		byTitle[task.Title] = task
	}

	claimed, err := store.ClaimNextReadyTasks(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	for _, c := range claimed {
		byTitle[c.Title] = c
	}

	taskA := byTitle["A"]
	updatedA, err := store.UpdateTaskState(ctx, taskA.ID, taskA.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("UpdateTaskState(A->RUNNING) error = %v", err)
	}
	_, err = store.UpdateTaskResult(ctx, updatedA.ID, updatedA.UpdatedAt, models.TaskResult{Success: true})
	if err != nil {
		t.Fatalf("UpdateTaskResult(A) error = %v", err)
	}

	taskB := byTitle["B"]
	updatedB, err := store.UpdateTaskState(ctx, taskB.ID, taskB.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("UpdateTaskState(B->RUNNING) error = %v", err)
	}
	_, err = store.UpdateTaskResult(ctx, updatedB.ID, updatedB.UpdatedAt, models.TaskResult{Success: false, Payload: "oops"})
	if err != nil {
		t.Fatalf("UpdateTaskResult(B) error = %v", err)
	}

	taskD := byTitle["D"]
	updatedD, err := store.UpdateTaskState(ctx, taskD.ID, taskD.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		t.Fatalf("UpdateTaskState(D->RUNNING) error = %v", err)
	}
	byTitle["D"] = *updatedD
}

func assertStateCounts(t *testing.T, counts map[models.TaskState]int) {
	t.Helper()
	wantCounts := map[models.TaskState]int{
		models.TaskStateReady:     1,
		models.TaskStateQueued:    1,
		models.TaskStateRunning:   1,
		models.TaskStateCompleted: 1,
		models.TaskStateFailed:    1,
	}
	for state, want := range wantCounts {
		if counts[state] != want {
			t.Errorf("count[%s] = %d, want %d", state, counts[state], want)
		}
	}
}

func assertStatusOutput(t *testing.T, counts map[models.TaskState]int) {
	t.Helper()
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := printStatus(cmd, counts); err != nil {
		t.Fatalf("printStatus() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "STATE             COUNT") {
		t.Error("missing header line")
	}
	for _, expect := range []string{
		"READY             1",
		"QUEUED            1",
		"RUNNING           1",
		"COMPLETED         1",
		"FAILED            1",
		"queue_length      2",
		"active_threads    1",
	} {
		if !strings.Contains(output, expect) {
			t.Errorf("output missing %q\nfull output:\n%s", expect, output)
		}
	}
}

func openInMemoryStore(t *testing.T) *kanban.Store {
	t.Helper()
	store, err := kanban.OpenStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
