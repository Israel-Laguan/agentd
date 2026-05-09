package testutil

import (
	"context"

	"agentd/internal/models"
)

func (s *FakeKanbanStore) MaterializePlan(_ context.Context, plan models.DraftPlan) (*models.Project, []models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project := models.Project{
		BaseEntity:    models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
		Name:          plan.ProjectName,
		OriginalInput: plan.Description,
		WorkspacePath: "/tmp/projects/" + plan.ProjectName,
	}
	s.projects[project.ID] = project
	tasks := make([]models.Task, 0, len(plan.Tasks))
	for _, draft := range plan.Tasks {
		task := models.Task{
			BaseEntity:  models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
			ProjectID:   project.ID,
			AgentID:     "default",
			Title:       draft.Title,
			Description: draft.Description,
			State:       models.TaskStateReady,
			Assignee:    draft.Assignee,
		}
		if task.Assignee == "" {
			task.Assignee = models.TaskAssigneeSystem
		}
		s.tasks[task.ID] = task
		tasks = append(tasks, task)
	}
	return &project, tasks, nil
}

func (s *FakeKanbanStore) GetProject(_ context.Context, id string) (*models.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, models.ErrProjectNotFound
	}
	return &p, nil
}

func (s *FakeKanbanStore) ListProjects(context.Context) ([]models.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	return out, nil
}

func (s *FakeKanbanStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.projects {
		if p.Name == "_system" {
			return &p, nil
		}
	}
	project := models.Project{
		BaseEntity: models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
		Name:       "_system",
	}
	s.projects[project.ID] = project
	return &project, nil
}

func (s *FakeKanbanStore) EnsureProjectTask(_ context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.ProjectID == projectID && t.Title == draft.Title && t.State != models.TaskStateCompleted {
			return &t, false, nil
		}
	}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
		ProjectID:   projectID,
		AgentID:     "default",
		Title:       draft.Title,
		Description: draft.Description,
		Assignee:    draft.Assignee,
		State:       models.TaskStateReady,
	}
	s.tasks[task.ID] = task
	return &task, true, nil
}
