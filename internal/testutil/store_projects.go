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
	tempToID := make(map[string]string, len(plan.Tasks))
	tasks := make([]models.Task, 0, len(plan.Tasks))
	for _, draft := range plan.Tasks {
		state := models.TaskStateReady
		if len(draft.DependsOn) > 0 || plan.WorkspacePending {
			state = models.TaskStatePending
		}
		task := models.Task{
			BaseEntity:      models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
			ProjectID:       project.ID,
			AgentID:         "default",
			Title:           draft.Title,
			Description:     draft.Description,
			State:           state,
			Assignee:        draft.Assignee,
			SuccessCriteria: append([]string(nil), draft.SuccessCriteria...),
		}
		if task.Assignee == "" {
			task.Assignee = models.TaskAssigneeSystem
		}
		s.tasks[task.ID] = task
		tasks = append(tasks, task)
		if tid := draft.ID(); tid != "" {
			tempToID[tid] = task.ID
		}
	}
	for i, draft := range plan.Tasks {
		for _, dep := range draft.DependsOn {
			if parentID, ok := tempToID[dep]; ok {
				s.childParents[tasks[i].ID] = append(s.childParents[tasks[i].ID], parentID)
			}
		}
	}
	return &project, tasks, nil
}

func (s *FakeKanbanStore) MarkProjectTasksReady(_ context.Context, projectID string) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var unlocked []models.Task
	for id, t := range s.tasks {
		if t.ProjectID != projectID || t.State != models.TaskStatePending {
			continue
		}
		if s.hasPendingParents(id) {
			continue
		}
		t.State = models.TaskStateReady
		t.UpdatedAt = now()
		s.tasks[id] = t
		unlocked = append(unlocked, t)
	}
	return unlocked, nil
}

func (s *FakeKanbanStore) hasPendingParents(taskID string) bool {
	parents := s.childParents[taskID]
	for _, pid := range parents {
		parent, ok := s.tasks[pid]
		if !ok {
			continue
		}
		if parent.State != models.TaskStateCompleted && parent.State != models.TaskStateFailed {
			return true
		}
	}
	return false
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
		BaseEntity:      models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
		ProjectID:       projectID,
		AgentID:         "default",
		Title:           draft.Title,
		Description:     draft.Description,
		Assignee:        draft.Assignee,
		State:           models.TaskStateReady,
		SuccessCriteria: append([]string(nil), draft.SuccessCriteria...),
	}
	s.tasks[task.ID] = task
	return &task, true, nil
}
