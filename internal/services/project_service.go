package services

import (
	"context"

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
func (s *ProjectService) MaterializePlan(
	ctx context.Context,
	plan models.DraftPlan,
) (*models.Project, []models.Task, error) {
	project, tasks, err := s.store.MaterializePlan(ctx, plan)
	if err != nil {
		return nil, nil, err
	}
	workspace, err := s.ws.EnsureProjectDir(ctx, project.ID)
	if err != nil {
		return nil, nil, err
	}
	project.WorkspacePath = workspace
	return project, tasks, nil
}
