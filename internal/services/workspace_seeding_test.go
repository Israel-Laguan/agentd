package services_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

// TestMaterializeWithSourcePath verifies Option A: when source_path is set,
// workspace content is copied before tasks become READY.
func TestMaterializeWithSourcePath(t *testing.T) {
	t.Parallel()

	// Setup: create a temp source directory with a test file.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "subdir", "nested.md"), []byte("# doc"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use a real FSWorkspaceManager with a temp root.
	wsRoot := t.TempDir()
	ws := &sandbox.FSWorkspaceManager{Root: wsRoot}
	store := testutil.NewFakeStore()
	svc := services.NewProjectService(store, ws)

	plan := models.DraftPlan{
		ProjectName: "source-test",
		SourcePath:  srcDir,
		Tasks: []models.DraftTask{
			{Title: "T1", Description: "do something"},
		},
	}

	project, tasks, err := svc.MaterializePlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}

	// Verify workspace was populated with source content.
	helloPath := filepath.Join(project.WorkspacePath, "hello.txt")
	data, err := os.ReadFile(helloPath)
	if err != nil {
		t.Fatalf("read hello.txt: %v", err)
	}
	if string(data) != "world" {
		t.Fatalf("hello.txt content = %q, want %q", data, "world")
	}

	nestedPath := filepath.Join(project.WorkspacePath, "subdir", "nested.md")
	data, err = os.ReadFile(nestedPath)
	if err != nil {
		t.Fatalf("read nested.md: %v", err)
	}
	if string(data) != "# doc" {
		t.Fatalf("nested.md content = %q, want %q", data, "# doc")
	}

	// Tasks should be READY since source_path was provided and copied.
	if len(tasks) != 1 {
		t.Fatalf("tasks count = %d, want 1", len(tasks))
	}
	if tasks[0].State != models.TaskStateReady {
		t.Fatalf("task state = %q, want READY", tasks[0].State)
	}
}

// TestMaterializeWithSourcePathAndDeps verifies that the response includes
// all tasks (both root READY and dependent PENDING) when source_path is set.
func TestMaterializeWithSourcePathAndDeps(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	wsRoot := t.TempDir()
	ws := &sandbox.FSWorkspaceManager{Root: wsRoot}
	store := testutil.NewFakeStore()
	svc := services.NewProjectService(store, ws)

	plan := models.DraftPlan{
		ProjectName: "deps-test",
		SourcePath:  srcDir,
		Tasks: []models.DraftTask{
			{TempID: "t1", Title: "Build"},
			{TempID: "t2", Title: "Test", DependsOn: []string{"t1"}},
		},
	}

	_, tasks, err := svc.MaterializePlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("tasks count = %d, want 2 (both root and dependent)", len(tasks))
	}

	byTitle := make(map[string]models.Task)
	for _, task := range tasks {
		byTitle[task.Title] = task
	}

	if byTitle["Build"].State != models.TaskStateReady {
		t.Fatalf("Build state = %q, want READY", byTitle["Build"].State)
	}
	if byTitle["Test"].State != models.TaskStatePending {
		t.Fatalf("Test state = %q, want PENDING (has dependency)", byTitle["Test"].State)
	}
}

// TestMaterializeWithoutSourcePath verifies Option B: tasks start PENDING
// when no source_path is provided.
func TestMaterializeWithoutSourcePath(t *testing.T) {
	t.Parallel()

	wsRoot := t.TempDir()
	ws := &sandbox.FSWorkspaceManager{Root: wsRoot}
	store := testutil.NewFakeStore()
	svc := services.NewProjectService(store, ws)

	plan := models.DraftPlan{
		ProjectName: "pending-test",
		Tasks: []models.DraftTask{
			{Title: "T1", Description: "do something"},
			{Title: "T2", Description: "do something else"},
		},
	}

	_, tasks, err := svc.MaterializePlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}

	// All tasks should be PENDING — workspace not ready.
	for i, task := range tasks {
		if task.State != models.TaskStatePending {
			t.Fatalf("task[%d] state = %q, want PENDING", i, task.State)
		}
	}
}

// TestWorkspaceReadyUnlocksTasks verifies that MarkWorkspaceReady transitions
// PENDING tasks to READY after workspace is populated.
func TestWorkspaceReadyUnlocksTasks(t *testing.T) {
	t.Parallel()

	wsRoot := t.TempDir()
	ws := &sandbox.FSWorkspaceManager{Root: wsRoot}
	store := testutil.NewFakeStore()
	svc := services.NewProjectService(store, ws)

	// Materialize without source_path — tasks PENDING.
	plan := models.DraftPlan{
		ProjectName: "ready-test",
		Tasks: []models.DraftTask{
			{Title: "T1", Description: "work"},
		},
	}
	project, _, err := svc.MaterializePlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}

	// Workspace is empty, so marking ready should fail.
	_, err = svc.MarkWorkspaceReady(context.Background(), project.ID)
	if err == nil {
		t.Fatal("MarkWorkspaceReady should fail on empty workspace")
	}

	// Simulate workspace seeding by writing a file.
	if err := os.WriteFile(filepath.Join(project.WorkspacePath, "data.txt"), []byte("seeded"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Now marking ready should succeed and unlock tasks.
	unlocked, err := svc.MarkWorkspaceReady(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("MarkWorkspaceReady: %v", err)
	}
	if len(unlocked) != 1 {
		t.Fatalf("unlocked count = %d, want 1", len(unlocked))
	}
	if unlocked[0].State != models.TaskStateReady {
		t.Fatalf("unlocked task state = %q, want READY", unlocked[0].State)
	}
}

// TestSourcePathNotDirectory verifies that a non-directory source_path is
// rejected with a clear error.
func TestSourcePathNotDirectory(t *testing.T) {
	t.Parallel()

	srcFile := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(srcFile, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	wsRoot := t.TempDir()
	ws := &sandbox.FSWorkspaceManager{Root: wsRoot}
	store := testutil.NewFakeStore()
	svc := services.NewProjectService(store, ws)

	plan := models.DraftPlan{
		ProjectName: "bad-source",
		SourcePath:  srcFile,
		Tasks:       []models.DraftTask{{Title: "T1"}},
	}

	_, _, err := svc.MaterializePlan(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error for non-directory source_path")
	}
}

// TestSourcePathNotExist verifies that a missing source_path is rejected.
func TestSourcePathNotExist(t *testing.T) {
	t.Parallel()

	wsRoot := t.TempDir()
	ws := &sandbox.FSWorkspaceManager{Root: wsRoot}
	store := testutil.NewFakeStore()
	svc := services.NewProjectService(store, ws)

	plan := models.DraftPlan{
		ProjectName: "missing-source",
		SourcePath:  "/nonexistent/path/that/does/not/exist",
		Tasks:       []models.DraftTask{{Title: "T1"}},
	}

	_, _, err := svc.MaterializePlan(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error for nonexistent source_path")
	}
}

// TestWorkspacePopulatedCheck verifies IsWorkspacePopulated behavior.
func TestWorkspacePopulatedCheck(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws := &sandbox.FSWorkspaceManager{Root: root}
	ctx := context.Background()

	dir, err := ws.EnsureProjectDir(ctx, "test-proj")
	if err != nil {
		t.Fatalf("EnsureProjectDir: %v", err)
	}

	// Empty workspace.
	populated, err := ws.IsWorkspacePopulated(ctx, "test-proj")
	if err != nil {
		t.Fatalf("IsWorkspacePopulated: %v", err)
	}
	if populated {
		t.Fatal("expected empty workspace to report not populated")
	}

	// Write a file.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	populated, err = ws.IsWorkspacePopulated(ctx, "test-proj")
	if err != nil {
		t.Fatalf("IsWorkspacePopulated: %v", err)
	}
	if !populated {
		t.Fatal("expected populated workspace after adding file")
	}
}

// TestClaimDoesNotReturnPendingTasks verifies that workers cannot claim
// PENDING tasks (race protection).
func TestClaimDoesNotReturnPendingTasks(t *testing.T) {
	t.Parallel()

	store := testutil.NewFakeStore()
	ctx := context.Background()

	// Materialize with WorkspacePending=true (no source_path via service).
	plan := models.DraftPlan{
		ProjectName:      "noclaim-test",
		WorkspacePending: true,
		Tasks: []models.DraftTask{
			{Title: "T1", Description: "should not be claimed"},
		},
	}
	_, tasks, err := store.MaterializePlan(ctx, plan)
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	if tasks[0].State != models.TaskStatePending {
		t.Fatalf("task state = %q, want PENDING", tasks[0].State)
	}

	// Attempt to claim — should get nothing since tasks are PENDING.
	claimed, err := store.ClaimNextReadyTasks(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed %d tasks, want 0 (PENDING tasks should not be claimable)", len(claimed))
	}

	// Mark tasks ready.
	unlocked, err := store.MarkProjectTasksReady(ctx, tasks[0].ProjectID)
	if err != nil {
		t.Fatalf("MarkProjectTasksReady: %v", err)
	}
	if len(unlocked) != 1 {
		t.Fatalf("unlocked count = %d, want 1", len(unlocked))
	}

	// Now claim should work.
	claimed, err = store.ClaimNextReadyTasks(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d tasks after unlock, want 1", len(claimed))
	}
}
