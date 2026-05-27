package services_test

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

type stubWorkspace struct {
	path string
	err  error
}

func (s *stubWorkspace) EnsureProjectDir(_ context.Context, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.path, nil
}

func (s *stubWorkspace) ProjectDir(projectID string) string { return "/tmp/" + projectID }

func (s *stubWorkspace) SecureDelete(context.Context, string) error { return nil }

func (s *stubWorkspace) SeedFromPath(context.Context, string, string) error { return nil }

func (s *stubWorkspace) IsWorkspacePopulated(context.Context, string) (bool, error) { return true, nil }

func TestProjectServiceMaterializeWorkspaceError(t *testing.T) {
	store := testutil.NewFakeStore()
	ws := &stubWorkspace{err: errors.New("disk full")}
	svc := services.NewProjectService(store, ws)

	plan := models.DraftPlan{ProjectName: "X", Description: "d", Tasks: nil}
	_, _, err := svc.MaterializePlan(context.Background(), plan)
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("MaterializePlan: %v", err)
	}
}

func TestProjectServiceMaterializeSetsWorkspacePath(t *testing.T) {
	store := testutil.NewFakeStore()
	ws := &stubWorkspace{path: "/workspaces/p1"}
	svc := services.NewProjectService(store, ws)

	plan := models.DraftPlan{ProjectName: "Demo", Description: "desc", Tasks: []models.DraftTask{{Title: "T1"}}}
	project, tasks, err := svc.MaterializePlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	if project.WorkspacePath != "/workspaces/p1" {
		t.Fatalf("WorkspacePath = %q", project.WorkspacePath)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d", len(tasks))
	}
	// Without source_path, tasks start PENDING (workspace not ready)
	if tasks[0].State != models.TaskStatePending {
		t.Fatalf("task state = %q, want PENDING", tasks[0].State)
	}
}
