//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestJ15_MCPBoardExportLargeBoard extends J15 past the silent 100-task cap
// (B-005). A board of 150 tasks must be exportable in full: the default
// board-wide call returns every task, and limit/offset page through a board
// larger than one page without dropping any.
func TestJ15_MCPBoardExportLargeBoard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J15-large [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)

	const taskCount = 150
	drafts := make([]DraftTask, taskCount)
	for i := range drafts {
		drafts[i] = DraftTask{Title: fmt.Sprintf("J15 large board task %03d", i), Description: "Exported via board.list_tasks."}
	}
	materialized := materializePlan(ctx, t, client, "J15-large", DraftPlan{
		ProjectName:         UniqueProjectName("j15-large"),
		Description:         "J15: a board larger than one default page exports completely",
		StartEmptyWorkspace: true,
		Tasks:               drafts,
	})
	// The 150 tasks are only exported, never run. Left READY they would sit in
	// the default profile's worker queue and starve every journey after this one
	// (J15-state, J11), so retire them when the test ends.
	t.Cleanup(func() { j15RetireProjectTasks(client, materialized.Project.ID) })

	// Default board-wide call: the whole 150-task board, not a 100-task page.
	var all []MCPTask
	if err := client.CallMCPTool(ctx, "board.list_tasks", nil, &all); err != nil {
		t.Fatalf("J15-large [mcp board.list_tasks] %v", err)
	}
	j15LargeAssertAllExported(ctx, t, client, materialized, all)

	// Paging: walk the board in pages of 100. The board-wide call spans every
	// project on the shared default profile, so the page total is not this
	// project's count — what matters is that every one of this project's tasks
	// appears exactly once across the pages (no gaps, no repeats).
	seen := make(map[string]bool)
	for offset := 0; ; offset += 100 {
		page := j15LargePage(ctx, t, client, 100, offset)
		if len(page) == 0 {
			break
		}
		for _, task := range page {
			if seen[task.ID] {
				t.Fatalf("J15-large [paging] task %s appeared on two pages", task.ID)
			}
			seen[task.ID] = true
		}
		if len(page) < 100 {
			break
		}
	}
	for _, want := range materialized.Tasks {
		if !seen[want.ID] {
			t.Fatalf("J15-large [paging] task %s (%q) missing from the paged export", want.ID, want.Title)
		}
	}
}

// j15LargeAssertAllExported asserts the default board-wide call returned every
// task of the materialized project, with real ids, titles and states.
func j15LargeAssertAllExported(ctx context.Context, t *testing.T, client *APIClient, materialized *MaterializeResult, all []MCPTask) {
	t.Helper()
	exported := make(map[string]MCPTask, len(all))
	for _, task := range all {
		if task.ID == "" || task.Title == "" || task.State == "" {
			t.Fatalf("J15-large [mcp board.list_tasks] task is missing metadata: %+v", task)
		}
		exported[task.ID] = task
	}
	for _, want := range materialized.Tasks {
		got, ok := exported[want.ID]
		if !ok {
			t.Fatalf("J15-large [mcp board.list_tasks] task %s (%q) missing from the default export (%d tasks returned)", want.ID, want.Title, len(all))
		}
		if got.Title != want.Title {
			t.Fatalf("J15-large [mcp board.list_tasks] task %s title = %q, want %q", want.ID, got.Title, want.Title)
		}
		if got.State != string(want.State) {
			t.Fatalf("J15-large [mcp board.list_tasks] task %s state = %q, want %q", want.ID, got.State, want.State)
		}
	}
	t.Logf("J15-large: default board-wide call exported %d/%d task(s)", len(exported), len(materialized.Tasks))
}

// j15LargePage fetches one page of the board-wide export.
func j15LargePage(ctx context.Context, t *testing.T, client *APIClient, limit, offset int) []MCPTask {
	t.Helper()
	var tasks []MCPTask
	if err := client.CallMCPTool(ctx, "board.list_tasks", map[string]any{"limit": limit, "offset": offset}, &tasks); err != nil {
		t.Fatalf("J15-large [mcp board.list_tasks limit=%d offset=%d] %v", limit, offset, err)
	}
	return tasks
}

// j15RetireProjectTasks moves every still-open task of a project to FAILED so
// the worker stops picking them up. It runs from t.Cleanup, after the test
// context is gone, so it uses its own. Best effort: a task the worker moved in
// the meantime (an invalid transition) is logged by nobody and skipped, since
// the goal is only to drain the backlog, not to assert on it.
func j15RetireProjectTasks(client *APIClient, projectID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var tasks []MCPTask
	args := map[string]any{"project_id": projectID, "limit": 500}
	if err := client.CallMCPTool(ctx, "board.list_tasks", args, &tasks); err != nil {
		return
	}
	for _, task := range tasks {
		switch TaskState(task.State) {
		case TaskStateCompleted, TaskStateFailed, TaskStateFailedRequiresHuman:
			continue
		}
		j15RetireTask(ctx, client, projectID, task.ID)
	}
}

// j15RetireTask PATCHes one task to FAILED. A non-2xx response is not a
// transport failure: HTTP 409 means a worker won the store's optimistic-lock
// race (TaskService.UpdateTaskState) and the task may still be open, so the
// task is re-read and retired again if it remains nonterminal. A terminal
// re-read — the worker finished the task, or reached a state FAILED cannot
// follow — stops the retry, as does a second non-2xx, so a task no worker
// claims can stay open without spinning the cleanup.
func j15RetireTask(ctx context.Context, client *APIClient, projectID, taskID string) {
	for attempt := 0; ; attempt++ {
		resp, err := client.Patch(ctx, "/api/v1/tasks/"+taskID, map[string]string{"state": string(TaskStateFailed)})
		if err != nil {
			return
		}
		ok := resp.StatusCode >= 200 && resp.StatusCode < 300
		_ = resp.Body.Close()
		if ok {
			return
		}
		if attempt >= 1 {
			return
		}
		state, err := j15ReadTaskState(ctx, client, projectID, taskID)
		if err != nil {
			return
		}
		switch state {
		case TaskStateCompleted, TaskStateFailed, TaskStateFailedRequiresHuman:
			return
		}
	}
}

// j15ReadTaskState re-reads one task's state from the project-scoped list;
// there is no per-task GET endpoint (see the Task type's doc comment).
func j15ReadTaskState(ctx context.Context, client *APIClient, projectID, taskID string) (TaskState, error) {
	resp, err := client.ListTasks(ctx, projectID, "", false)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list tasks returned %d", resp.StatusCode)
	}
	var envelope struct {
		Data []Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", err
	}
	for _, task := range envelope.Data {
		if task.ID == taskID {
			return task.State, nil
		}
	}
	return "", fmt.Errorf("task %s not found in project %s", taskID, projectID)
}
