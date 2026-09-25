package services

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// ProjectService materializes approved DraftPlans into board state and
// ensures the project workspace exists on disk.
type ProjectService struct {
	store models.KanbanStore
	ws    sandbox.WorkspaceManager
}

// NewProjectService wires the store and workspace manager required to
// materialize plans.
func NewProjectService(store models.KanbanStore, ws sandbox.WorkspaceManager) *ProjectService {
	return &ProjectService{store: store, ws: ws}
}

// MaterializePlan persists the project and its tasks, then provisions the
// workspace directory so workers can execute commands inside it.
//
// When plan.SourcePath is set, the service copies the directory contents
// into the workspace before returning — eliminating the race between
// workspace seeding and task dispatch.
//
// When plan.SourcePath is empty, tasks are created in PENDING state and
// remain unclaimable until the operator calls MarkWorkspaceReady.
func (s *ProjectService) MaterializePlan(
	ctx context.Context,
	plan models.DraftPlan,
) (*models.Project, []models.Task, error) {
	sourcePath := strings.TrimSpace(plan.SourcePath)
	needsExplicitReady := sourcePath == "" && !plan.StartEmptyWorkspace
	plan.WorkspacePending = sourcePath != "" || !plan.StartEmptyWorkspace
	if sourcePath == "" {
		plan.SourcePath = ""
	}

	project, tasks, err := s.store.MaterializePlan(ctx, plan)
	if err != nil {
		slog.Error("materialize plan failed", "error", err, "project_name", plan.ProjectName)
		return nil, nil, err
	}
	if err := s.ensureWorkspace(ctx, *project); err != nil {
		slog.Error("ensure project workspace failed", "project_id", project.ID, "error", err)
		return nil, nil, err
	}

	if plan.SourcePath != "" {
		if err := s.ws.SeedFromPath(ctx, project.ID, plan.SourcePath); err != nil {
			slog.Error("seed workspace from source_path failed", "project_id", project.ID, "source_path", plan.SourcePath, "error", err)
			return nil, nil, fmt.Errorf("seed workspace from source_path: %w", err)
		}
	}

	if needsExplicitReady {
		// Tasks stay PENDING until the operator signals workspace readiness.
		// Transition them now so callers can see they need to call
		// MarkWorkspaceReady before tasks become claimable.
		slog.Info("materialize plan: workspace pending explicit ready", "project_id", project.ID, "task_count", len(tasks))
		return project, tasks, nil
	}

	// source_path was provided and copied; unlock root tasks to READY.
	unlocked, err := s.store.MarkProjectTasksReady(ctx, project.ID)
	if err != nil {
		slog.Error("unlock tasks after workspace seed failed", "project_id", project.ID, "error", err)
		return nil, nil, fmt.Errorf("unlock tasks after workspace seed: %w", err)
	}
	slog.Info("materialize plan: workspace seeded and tasks unlocked", "project_id", project.ID, "task_count", len(tasks), "unlocked_count", len(unlocked))
	// Merge updated states back into the full task list so dependent
	// tasks (still PENDING) are not dropped from the response.
	ready := make(map[string]models.Task, len(unlocked))
	for _, t := range unlocked {
		ready[t.ID] = t
	}
	for i, t := range tasks {
		if updated, ok := ready[t.ID]; ok {
			tasks[i] = updated
		}
	}
	return project, tasks, nil
}

func (s *ProjectService) ensureWorkspace(ctx context.Context, project models.Project) error {
	workspace, err := s.ws.EnsureProjectDir(ctx, project.ID)
	if err != nil {
		return err
	}
	workspace, err = filepath.Abs(filepath.Clean(workspace))
	if err != nil {
		return fmt.Errorf("resolve persisted workspace path: %w", err)
	}
	if !filepath.IsAbs(project.WorkspacePath) || filepath.Clean(project.WorkspacePath) != workspace {
		return fmt.Errorf("persisted workspace path %q does not match provisioned workspace %q", project.WorkspacePath, workspace)
	}
	return nil
}

// MarkWorkspaceReady transitions all PENDING tasks for the given project to
// READY, signaling that the workspace has been populated and workers may
// claim tasks.
func (s *ProjectService) MarkWorkspaceReady(ctx context.Context, projectID string) ([]models.Task, error) {
	populated, err := s.ws.IsWorkspacePopulated(ctx, projectID)
	if err != nil {
		slog.Error("check workspace populated failed", "project_id", projectID, "error", err)
		return nil, fmt.Errorf("check workspace populated: %w", err)
	}
	if !populated {
		slog.Warn("mark workspace ready called but workspace is empty", "project_id", projectID)
		return nil, fmt.Errorf("%w: workspace is empty; seed content before marking ready", models.ErrWorkspaceNotReady)
	}
	tasks, err := s.store.MarkProjectTasksReady(ctx, projectID)
	if err != nil {
		slog.Error("mark project tasks ready failed", "project_id", projectID, "error", err)
		return nil, err
	}
	slog.Info("workspace marked ready and tasks unlocked", "project_id", projectID, "unlocked_count", len(tasks))
	return tasks, nil
}
