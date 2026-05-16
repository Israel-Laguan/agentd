package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/services"
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
	getProject *models.Project
	getProjErr error
	getTask    *models.Task
	getTaskErr error
	addCalls   int
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
	m.addCalls++
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
	return &updated, nil
}

func (m *minimalStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
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
func (m *minimalStore) BlockTaskWithSubtasks(context.Context, string, time.Time, []models.DraftTask) (*models.Task, []models.Task, error) {
	return nil, nil, nil
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
func (f fullStore) AssignTaskAgent(context.Context, string, time.Time, string) (*models.Task, error) {
	return nil, nil
}
func (f fullStore) ListSettings(context.Context) ([]models.Setting, error)   { return nil, nil }
func (f fullStore) GetSetting(context.Context, string) (string, bool, error) { return "", false, nil }
func (f fullStore) SetSetting(context.Context, string, string) error         { return nil }
func (f fullStore) Close() error                                             { return nil }

var _ models.KanbanStore = fullStore{}

func newStore() (*minimalStore, fullStore) {
	m := &minimalStore{}
	return m, fullStore{m}
}

func TestAddHumanCommentDelegatesToBoardWithoutPrecheck(t *testing.T) {
	store, full := newStore()
	board := &store.stubBoard
	svc := services.NewTaskService(full, board)

	comment, err := svc.AddHumanComment(context.Background(), "task-1", "  please pause  ")
	if err != nil {
		t.Fatalf("AddHumanComment: %v", err)
	}
	if board.addCalls != 1 {
		t.Fatalf("board.addCalls = %d, want 1", board.addCalls)
	}
	if board.lastTaskID != "task-1" {
		t.Fatalf("board.lastTaskID = %q, want task-1", board.lastTaskID)
	}
	if board.lastComment.Author != models.CommentAuthorUser {
		t.Fatalf("comment author = %q, want USER", board.lastComment.Author)
	}
	if board.lastComment.Body != "please pause" || board.lastComment.Content != "please pause" {
		t.Fatalf("comment body trimmed mismatch: %+v", board.lastComment)
	}
	if comment.TaskID != "task-1" {
		t.Fatalf("returned comment task id = %q", comment.TaskID)
	}
}

func TestAddHumanCommentRejectsEmptyContent(t *testing.T) {
	store, full := newStore()
	board := &store.stubBoard
	svc := services.NewTaskService(full, board)

	_, err := svc.AddHumanComment(context.Background(), "task-1", "   ")
	if !errors.Is(err, models.ErrInvalidDraftPlan) {
		t.Fatalf("expected ErrInvalidDraftPlan for empty content, got %v", err)
	}
	if board.addCalls != 0 {
		t.Fatalf("board.addCalls = %d, expected no call on empty content", board.addCalls)
	}
}

func TestAddHumanCommentPropagatesStateConflict(t *testing.T) {
	store, full := newStore()
	board := &store.stubBoard
	board.addErr = models.ErrStateConflict
	svc := services.NewTaskService(full, board)

	_, err := svc.AddHumanComment(context.Background(), "task-1", "stop")
	if !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("expected ErrStateConflict propagation, got %v", err)
	}
}

func TestAddHumanCommentFallsBackToStoreWithoutBoard(t *testing.T) {
	store, full := newStore()
	svc := services.NewTaskService(full, nil)

	_, err := svc.AddHumanComment(context.Background(), "task-2", "noted")
	if err != nil {
		t.Fatalf("AddHumanComment fallback: %v", err)
	}
	if store.addCalls != 1 {
		t.Fatalf("store.addCalls = %d, want 1", store.addCalls)
	}
	if got := store.lastComment.Body; got != "noted" {
		t.Fatalf("store last comment body = %q, want noted", got)
	}
}

func TestUpdateTaskStateValidatesIncomingState(t *testing.T) {
	store, full := newStore()
	now := time.Now().UTC()
	store.getTask = &models.Task{BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: now}, State: models.TaskStateRunning}
	svc := services.NewTaskService(full, &store.stubBoard)

	if _, err := svc.UpdateTaskState(context.Background(), "task-1", models.TaskState("BOGUS")); !errors.Is(err, models.ErrInvalidStateTransition) {
		t.Fatalf("expected invalid state error, got %v", err)
	}

	updated, err := svc.UpdateTaskState(context.Background(), "task-1", models.TaskStateCompleted)
	if err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	if updated.State != models.TaskStateCompleted {
		t.Fatalf("state = %q, want COMPLETED", updated.State)
	}
}

func TestListByProjectRequiresKnownProject(t *testing.T) {
	store, full := newStore()
	svc := services.NewTaskService(full, &store.stubBoard)

	_, err := svc.ListByProject(context.Background(), "missing", models.TaskFilter{})
	if !errors.Is(err, models.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestListByProjectInjectsProjectIDFilter(t *testing.T) {
	store, full := newStore()
	store.getProject = &models.Project{BaseEntity: models.BaseEntity{ID: "p1"}}
	store.listResult = models.PaginatedResult[models.Task]{Total: 0, Data: nil}
	svc := services.NewTaskService(full, &store.stubBoard)

	_, err := svc.ListByProject(context.Background(), "p1", models.TaskFilter{})
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if store.listFilter.ProjectID == nil || *store.listFilter.ProjectID != "p1" {
		t.Fatalf("project filter not injected: %+v", store.listFilter.ProjectID)
	}
}
