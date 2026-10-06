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
	plan.SourcePath = sourcePath

	staged, err := s.stageSeed(ctx, plan.SourcePath)
	if err != nil {
		return nil, nil, err
	}
	if staged != nil {
		defer staged.Discard()
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

	if staged != nil {
		if err := staged.Promote(ctx, project.ID); err != nil {
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

// stageSeed copies source_path into staging before anything is persisted.
// store.MaterializePlan commits project and task rows in its own transaction, so
// anything that can fail after it leaves an orphan project with PENDING tasks
// that nothing can unlock. A bad path is rejected up front (ErrInvalidDraftPlan
// maps to 400); a copy that fails partway fails here, before any row exists
// (B-021). Only the swap of the finished copy into the project's directory
// happens afterwards. An empty sourcePath stages nothing and returns nil.
func (s *ProjectService) stageSeed(ctx context.Context, sourcePath string) (sandbox.StagedSeed, error) {
	if sourcePath == "" {
		return nil, nil
	}
	if _, err := sandbox.ValidateSourcePath(sourcePath); err != nil {
		slog.Warn("materialize plan: invalid source_path", "source_path", sourcePath, "error", err)
		return nil, fmt.Errorf("%w: source_path: %w", models.ErrInvalidDraftPlan, err)
	}
	staged, err := s.ws.StageSeed(ctx, sourcePath)
	if err != nil {
		slog.Error("stage workspace seed from source_path failed", "source_path", sourcePath, "error", err)
		return nil, fmt.Errorf("seed workspace from source_path: %w", err)
	}
	return staged, nil
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
	// Reaching here means the operator put content in the directory themselves, so
	// the project no longer started empty and its content is not a leftover to be
	// cleaned away by recovery. The bit records "this workspace was created empty and
	// nothing else was put in it", and it is mandatory to clear before the tasks are
	// unlocked: a failed clear leaves started_empty set, which would make a later
	// boot recovery eligible to delete that hand-seeded content, so refuse to unlock
	// the tasks and let the caller retry — the call is idempotent.
	if err := s.store.ClearProjectStartedEmpty(ctx, projectID); err != nil {
		slog.Error("clear started_empty failed", "project_id", projectID, "error", err)
		return nil, fmt.Errorf("clear started_empty: %w", err)
	}
	tasks, err := s.store.MarkProjectTasksReady(ctx, projectID)
	if err != nil {
		slog.Error("mark project tasks ready failed", "project_id", projectID, "error", err)
		return nil, err
	}
	slog.Info("workspace marked ready and tasks unlocked", "project_id", projectID, "unlocked_count", len(tasks))
	return tasks, nil
}
