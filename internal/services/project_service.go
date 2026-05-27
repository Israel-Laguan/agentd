package services

import (
	"context"
	"fmt"
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
	plan.WorkspacePending = true
	needsExplicitReady := strings.TrimSpace(plan.SourcePath) == ""
	if needsExplicitReady {
		plan.SourcePath = ""
	}

	project, tasks, err := s.store.MaterializePlan(ctx, plan)
	if err != nil {
		return nil, nil, err
	}
	workspace, err := s.ws.EnsureProjectDir(ctx, project.ID)
	if err != nil {
		return nil, nil, err
	}
	project.WorkspacePath = workspace

	if plan.SourcePath != "" {
		if err := s.ws.SeedFromPath(ctx, project.ID, plan.SourcePath); err != nil {
			return nil, nil, fmt.Errorf("seed workspace from source_path: %w", err)
		}
	}

	if needsExplicitReady {
		// Tasks stay PENDING until the operator signals workspace readiness.
		// Transition them now so callers can see they need to call
		// MarkWorkspaceReady before tasks become claimable.
		return project, tasks, nil
	}

	// source_path was provided and copied; unlock tasks to READY.
	unlocked, err := s.store.MarkProjectTasksReady(ctx, project.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("unlock tasks after workspace seed: %w", err)
	}
	if len(unlocked) > 0 {
		tasks = unlocked
	}
	return project, tasks, nil
}

// MarkWorkspaceReady transitions all PENDING tasks for the given project to
// READY, signaling that the workspace has been populated and workers may
// claim tasks.
func (s *ProjectService) MarkWorkspaceReady(ctx context.Context, projectID string) ([]models.Task, error) {
	populated, err := s.ws.IsWorkspacePopulated(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("check workspace populated: %w", err)
	}
	if !populated {
		return nil, fmt.Errorf("%w: workspace is empty; seed content before marking ready", models.ErrWorkspaceNotReady)
	}
	return s.store.MarkProjectTasksReady(ctx, projectID)
}
