package services_test

import (
	"context"
	"time"

	"agentd/internal/models"
)

type stubBoard struct {
	addCalls    int
	lastComment models.Comment
	lastTaskID  string
	addErr      error
	listResult  models.PaginatedResult[models.Task]
	listErr     error
	listFilter  models.TaskFilter
}

func (b *stubBoard) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}

func (b *stubBoard) ListProjectsPage(context.Context, models.PaginationParams) (models.PaginatedResult[models.Project], error) {
	return models.PaginatedResult[models.Project]{}, nil
}

func (b *stubBoard) ListTasks(_ context.Context, filter models.TaskFilter) (models.PaginatedResult[models.Task], error) {
	b.listFilter = filter
	return b.listResult, b.listErr
}

func (b *stubBoard) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) {
	return nil, nil
}

func (b *stubBoard) UpdateTaskResult(context.Context, string, time.Time, models.TaskResult) (*models.Task, error) {
	return nil, nil
}

func (b *stubBoard) AddCommentAndPause(_ context.Context, taskID string, comment models.Comment) error {
	b.addCalls++
	b.lastTaskID = taskID
	b.lastComment = comment
	return b.addErr
}

func (b *stubBoard) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}

type minimalStore struct {
	stubBoard
	getProject    *models.Project
	getProjErr    error
	getTask       *models.Task
	getTaskErr    error
	patchCalls    int
	assignErr     error
	splitErr      error
	splitChildren []models.Task
}

func (m *minimalStore) GetProject(_ context.Context, id string) (*models.Project, error) {
	if m.getProjErr != nil {
		return nil, m.getProjErr
	}
	if m.getProject != nil && m.getProject.ID == id {
		return m.getProject, nil
	}
	return nil, models.ErrProjectNotFound
}

func (m *minimalStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	if m.getTaskErr != nil {
		return nil, m.getTaskErr
	}
	if m.getTask != nil && m.getTask.ID == id {
		return m.getTask, nil
	}
	return nil, models.ErrTaskNotFound
}

func (m *minimalStore) AddComment(_ context.Context, c models.Comment) error {
	m.stubBoard.addCalls++
	m.lastComment = c
	return nil
}

func (m *minimalStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (m *minimalStore) UpdateTaskState(_ context.Context, _ string, _ time.Time, next models.TaskState) (*models.Task, error) {
	if m.getTask == nil {
		return nil, models.ErrTaskNotFound
	}
	updated := *m.getTask
	updated.State = next
	m.getTask = &updated
	return &updated, nil
}

func (m *minimalStore) UpdateTaskDescription(_ context.Context, _ string, _ time.Time, description string) (*models.Task, error) {
	if m.getTask == nil {
		return nil, models.ErrTaskNotFound
	}
	updated := *m.getTask
	updated.Description = description
	m.getTask = &updated
	return &updated, nil
}

func (m *minimalStore) UpdateTaskPatch(_ context.Context, _ string, _ time.Time, state *models.TaskState, description *string) (*models.Task, error) {
	m.patchCalls++
	if m.getTask == nil {
		return nil, models.ErrTaskNotFound
	}
	updated := *m.getTask
	if state != nil {
		updated.State = *state
	}
	if description != nil {
		updated.Description = *description
	}
	m.getTask = &updated
	return &updated, nil
}

func (m *minimalStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}
func (m *minimalStore) MarkProjectTasksReady(context.Context, string) ([]models.Task, error) {
	return nil, nil
}
func (m *minimalStore) EnsureSystemProject(context.Context) (*models.Project, error) { return nil, nil }
func (m *minimalStore) EnsureProjectTask(context.Context, string, models.DraftTask) (*models.Task, bool, error) {
	return nil, false, nil
}
func (m *minimalStore) ListProjects(context.Context) ([]models.Project, error) { return nil, nil }
func (m *minimalStore) MarkTaskRunning(context.Context, string, time.Time, int) (*models.Task, error) {
	return nil, nil
}
func (m *minimalStore) UpdateTaskHeartbeat(context.Context, string) error { return nil }
func (m *minimalStore) IncrementRetryCount(context.Context, string, time.Time) (*models.Task, error) {
	return nil, nil
}
func (m *minimalStore) ReconcileOrphanedQueued(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *minimalStore) ReconcileStaleTasks(context.Context, []int, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (m *minimalStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, drafts []models.DraftTask) (*models.Task, []models.Task, error) {
	if m.splitErr != nil {
		return nil, nil, m.splitErr
	}
	if m.getTask == nil {
		return nil, nil, models.ErrTaskNotFound
	}
	parent := *m.getTask
	parent.State = models.TaskStateBlocked
	m.getTask = &parent
	if len(m.splitChildren) > 0 {
		return &parent, append([]models.Task(nil), m.splitChildren...), nil
	}
	children := make([]models.Task, 0, len(drafts))
	for _, d := range drafts {
		children = append(children, models.Task{
			BaseEntity:  models.BaseEntity{ID: "child-" + d.Title},
			Title:       d.Title,
			Description: d.Description,
			State:       models.TaskStateReady,
		})
	}
	return &parent, children, nil
}

func (m *minimalStore) AssignTaskAgent(_ context.Context, _ string, _ time.Time, agentID string) (*models.Task, error) {
	if m.assignErr != nil {
		return nil, m.assignErr
	}
	if m.getTask == nil {
		return nil, models.ErrTaskNotFound
	}
	updated := *m.getTask
	updated.AgentID = agentID
	m.getTask = &updated
	return &updated, nil
}

func (m *minimalStore) ListParentTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (m *minimalStore) ListChildTasks(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (m *minimalStore) ReconcileExpiredBlockedTasks(context.Context, time.Time) ([]models.Task, error) {
	return nil, nil
}
func (m *minimalStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}

// fullStore embeds minimalStore and adds no-op implementations for
// the remaining KanbanStore surface that this test does not exercise.
type fullStore struct {
	*minimalStore
}

func (f fullStore) ListComments(context.Context, string) ([]models.Comment, error) { return nil, nil }
func (f fullStore) UpdateCriteriaMet(context.Context, string, []string) error       { return nil }
func (f fullStore) ListCommentsSince(context.Context, string, time.Time) ([]models.Comment, error) {
	return nil, nil
}
func (f fullStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}
func (f fullStore) MarkCommentProcessed(context.Context, string, string) error { return nil }
func (f fullStore) AppendEvent(context.Context, models.Event) error            { return nil }
func (f fullStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}
func (f fullStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (f fullStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (f fullStore) ListCompletedTasksOlderThan(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}
func (f fullStore) RecordMemory(context.Context, models.Memory) error { return nil }
func (f fullStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}
func (f fullStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}
func (f fullStore) TouchMemories(context.Context, []string) error             { return nil }
func (f fullStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (f fullStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}
func (f fullStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return nil, nil
}
func (f fullStore) UpsertAgentProfile(context.Context, models.AgentProfile) error { return nil }
func (f fullStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return nil, nil
}
func (f fullStore) DeleteAgentProfile(context.Context, string) error { return nil }
func (f fullStore) ListSettings(context.Context) ([]models.Setting, error)   { return nil, nil }
func (f fullStore) GetSetting(context.Context, string) (string, bool, error) { return "", false, nil }
func (f fullStore) SetSetting(context.Context, string, string) error         { return nil }
func (f fullStore) Close() error                                             { return nil }

var _ models.KanbanStore = fullStore{}

func newStore() (*minimalStore, fullStore) {
	m := &minimalStore{}
	return m, fullStore{m}
}
