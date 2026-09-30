//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestJ05_MaterializeEdgeCases tests J05: the materialization edge cases
// around workspace readiness and source_path.
//
// The journey spec's step 1 ("materialize without workspace; expect 409") was
// written against an API that does not exist: materialize never 409s. A plan
// with neither source_path nor start_empty_workspace is *accepted* (201) and
// comes back with its tasks PENDING — the workspace gate is enforced on
// dispatch, and 409 is the code you get from workspace/ready when the
// workspace is still empty. That split is what this journey pins:
//
//  1. materialize with no workspace intent → 201, tasks PENDING (locked)
//  2. workspace/ready before seeding → 409 STATE_CONFLICT
//  3. workspace/ready after seeding → 200, root tasks READY
//  4. workspace/ready again → 200, same READY set (idempotent, not an error)
//  5. bad source_path (missing dir, and a file not a dir) → 400 VALIDATION
//     with no project row left behind
//  6. good source_path → 201, seeded, root tasks READY
//
// Step 5 is the regression guard for a real defect this journey found: the
// service used to persist the project and task rows *before* seeding the
// workspace, so a bad source_path returned 500 and orphaned a project whose
// PENDING tasks could never be unlocked (see B-004).
func TestJ05_MaterializeEdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J05 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)
	devenv := NewDevenvManager(composePath, "default")

	locked := materializePlan(ctx, t, client, "J05", DraftPlan{
		ProjectName: UniqueProjectName("j05-locked"),
		Description: "J05: no workspace intent, so tasks start PENDING",
		Tasks: []DraftTask{
			{Title: "J05 locked task", Description: "Must stay PENDING until the workspace is ready."},
		},
	})
	for _, task := range locked.Tasks {
		if task.State != TaskStatePending {
			t.Fatalf("J05 [materialize] task %s state = %s, want PENDING (no source_path, no start_empty_workspace)", task.ID, task.State)
		}
	}
	j05WorkspaceReadyIs409BeforeSeeding(ctx, t, client, locked.Project.ID)
	j05WorkspaceReadyIsIdempotent(ctx, t, client, devenv, locked.Project.ID)
	j05RejectsBadSourcePath(ctx, t, client, UniqueProjectName("j05-missing"))
	j05RejectsSourcePathThatIsAFile(ctx, t, client, devenv)
	j05SeedsFromGoodSourcePath(ctx, t, client, devenv)
}

// j05WorkspaceReadyIs409BeforeSeeding is step 2: marking an empty workspace
// ready is the conflict case. 409 is the only place this journey can observe
// the "workspace not ready" state — materialize itself accepts the plan.
func j05WorkspaceReadyIs409BeforeSeeding(ctx context.Context, t *testing.T, client *APIClient, projectID string) {
	t.Helper()

	resp, err := client.WorkspaceReady(ctx, projectID)
	if err != nil {
		t.Fatalf("J05 [workspace/ready] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		_ = resp.Body.Close()
		t.Fatalf("J05 [workspace/ready] on an unseeded workspace returned %d, want 409", resp.StatusCode)
	}
	apiErr, err := DecodeError(resp)
	if err != nil {
		t.Fatalf("J05 [workspace/ready] decode failed: %v", err)
	}
	if apiErr.Code != "STATE_CONFLICT" {
		t.Fatalf("J05 [workspace/ready] error code = %q, want STATE_CONFLICT (message: %s)", apiErr.Code, apiErr.Message)
	}

	// The 409 must leave the tasks locked, not half-unlocked.
	tasks, err := NewTaskPoller(client, projectID).listTasks(ctx, false)
	if err != nil {
		t.Fatalf("J05 [tasks] list failed: %v", err)
	}
	for _, task := range tasks {
		if task.State != TaskStatePending {
			t.Fatalf("J05 [tasks] after a 409, task %s state = %s, want PENDING", task.ID, task.State)
		}
	}
	t.Logf("J05 [workspace/ready] unseeded workspace correctly 409s: %s", apiErr.Message)
}

// j05WorkspaceReadyIsIdempotent is steps 3-4: seed, then mark ready twice.
// The contract is idempotent-200, not an error on the second call — the
// unlock UPDATE is scoped to `state = PENDING`, so nothing is double-
// unlocked and the second response is the same READY set.
func j05WorkspaceReadyIsIdempotent(ctx context.Context, t *testing.T, client *APIClient, devenv *DevenvManager, projectID string) {
	t.Helper()

	if err := devenv.SeedWorkspace(ctx, projectID); err != nil {
		t.Fatalf("J05 [seed workspace] failed: %v", err)
	}

	first, err := workspaceReadyTasks(ctx, t, client, projectID, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspaceReadyTasks(ctx, t, client, projectID, "second")
	if err != nil {
		t.Fatal(err)
	}

	if len(first) == 0 {
		t.Fatal("J05 [workspace/ready] first call unlocked no tasks after seeding")
	}
	firstStates := taskStatesByID(first)
	secondStates := taskStatesByID(second)
	if len(firstStates) != len(secondStates) {
		t.Fatalf("J05 [workspace/ready] second call returned %d READY task(s), first returned %d — not idempotent", len(secondStates), len(firstStates))
	}
	for id, state := range firstStates {
		if secondStates[id] != state {
			t.Fatalf("J05 [workspace/ready] task %s state changed across an idempotent repeat: %s -> %s", id, state, secondStates[id])
		}
	}
	t.Logf("J05 [workspace/ready] double-ready is idempotent: %d task(s) READY, unchanged", len(firstStates))
}

// j05RejectsBadSourcePath is step 5a: a source_path naming a directory that
// does not exist. The rejection must be a client error *and* must leave no
// project behind — pre-fix this was a 500 with an orphan row.
func j05RejectsBadSourcePath(ctx context.Context, t *testing.T, client *APIClient, projectName string) {
	t.Helper()

	badPath := "/home/agentd/sources/j05-does-not-exist-" + projectName
	resp, err := client.MaterializePlan(ctx, DraftPlan{
		ProjectName: projectName,
		Description: "J05: source_path names a directory that does not exist",
		SourcePath:  badPath,
		Tasks:       []DraftTask{{Title: "J05 bad path task"}},
	})
	if err != nil {
		t.Fatalf("J05 [materialize bad source_path] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		_ = resp.Body.Close()
		t.Fatalf("J05 [materialize bad source_path] returned %d, want 400 (a missing source is a client error)", resp.StatusCode)
	}
	apiErr, err := DecodeError(resp)
	if err != nil {
		t.Fatalf("J05 [materialize bad source_path] decode failed: %v", err)
	}
	if apiErr.Code != "VALIDATION_FAILED" {
		t.Fatalf("J05 [materialize bad source_path] error code = %q, want VALIDATION_FAILED (message: %s)", apiErr.Code, apiErr.Message)
	}
	j05AssertNoProjectRow(ctx, t, client, projectName)
	t.Logf("J05 [materialize] bad source_path rejected: %s", apiErr.Message)
}

// j05RejectsSourcePathThatIsAFile is step 5b: the path exists but is not a
// directory, a distinct rejection from a missing path.
func j05RejectsSourcePathThatIsAFile(ctx context.Context, t *testing.T, client *APIClient, devenv *DevenvManager) {
	t.Helper()

	projectName := UniqueProjectName("j05-notdir")
	filePath, err := devenv.CreateSourceFile(ctx, projectName)
	if err != nil {
		t.Fatalf("J05 [materialize] could not stage a source file: %v", err)
	}
	resp, err := client.MaterializePlan(ctx, DraftPlan{
		ProjectName: projectName,
		Description: "J05: source_path names a file, not a directory",
		SourcePath:  filePath,
		Tasks:       []DraftTask{{Title: "J05 not-a-directory task"}},
	})
	if err != nil {
		t.Fatalf("J05 [materialize file source_path] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		_ = resp.Body.Close()
		t.Fatalf("J05 [materialize file source_path] returned %d, want 400", resp.StatusCode)
	}
	apiErr, err := DecodeError(resp)
	if err != nil {
		t.Fatalf("J05 [materialize file source_path] decode failed: %v", err)
	}
	j05AssertNoProjectRow(ctx, t, client, projectName)
	t.Logf("J05 [materialize] file source_path rejected: %s", apiErr.Message)
}

// j05SeedsFromGoodSourcePath is step 6: the happy source_path path, which
// must be unaffected by the new pre-validation — the workspace is seeded
// synchronously and root tasks are unlocked with no workspace/ready call.
func j05SeedsFromGoodSourcePath(ctx context.Context, t *testing.T, client *APIClient, devenv *DevenvManager) {
	t.Helper()

	projectName := UniqueProjectName("j05-source")
	sourcePath, err := devenv.CreateSourceDir(ctx, projectName)
	if err != nil {
		t.Fatalf("J05 [materialize] could not stage a source dir: %v", err)
	}
	materialized := materializePlan(ctx, t, client, "J05", DraftPlan{
		ProjectName: projectName,
		Description: "J05: source_path seeds the workspace and unlocks root tasks",
		SourcePath:  sourcePath,
		Tasks:       []DraftTask{{Title: "J05 seeded task", Description: "Runs with a pre-seeded workspace."}},
	})
	if materialized.Tasks[0].State != TaskStateReady {
		t.Fatalf("J05 [materialize] task %s state = %s, want READY (source_path seeds the workspace)", materialized.Tasks[0].ID, materialized.Tasks[0].State)
	}
	t.Logf("J05 [materialize] source_path seeded workspace and unlocked %d task(s)", len(materialized.Tasks))
}

// j05AssertNoProjectRow is the regression assertion: a rejected materialize
// must not persist a project. Pre-fix, MaterializePlan committed the rows
// before seeding, so the project survived the 500 (B-004).
func j05AssertNoProjectRow(ctx context.Context, t *testing.T, client *APIClient, projectName string) {
	t.Helper()

	projects, err := ListProjects(ctx, client)
	if err != nil {
		t.Fatalf("J05 [projects] list failed: %v", err)
	}
	for _, p := range projects {
		if p.Name == projectName {
			t.Fatalf("J05 [projects] rejected materialize left project %q behind (orphan row, B-004)", p.Name)
		}
	}
}

func workspaceReadyTasks(ctx context.Context, t *testing.T, client *APIClient, projectID, which string) ([]Task, error) {
	t.Helper()

	resp, err := client.WorkspaceReady(ctx, projectID)
	if err != nil {
		t.Fatalf("J05 [workspace/ready %s] request failed: %v", which, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("J05 [workspace/ready %s] returned %d, want 200", which, resp.StatusCode)
	}
	ready, err := DecodeWorkspaceReady(resp)
	if err != nil {
		t.Fatalf("J05 [workspace/ready %s] decode failed: %v", which, err)
	}
	for _, task := range ready.Tasks {
		if task.State != TaskStateReady {
			t.Fatalf("J05 [workspace/ready %s] returned task %s in state %s, want only READY tasks", which, task.ID, task.State)
		}
	}
	return ready.Tasks, nil
}

// taskStatesByID indexes tasks by ID for comparing two snapshots of the same
// project — used to assert an idempotent repeat changed nothing.
func taskStatesByID(tasks []Task) map[string]TaskState {
	byID := make(map[string]TaskState, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task.State
	}
	return byID
}
