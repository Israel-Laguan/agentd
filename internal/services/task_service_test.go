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
	getProject    *models.Project
	getProjErr    error
	getTask       *models.Task
	getTaskErr    error
	addCalls      int
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

func TestListByProjectBoardPassesIncludeHealing(t *testing.T) {
	store, full := newStore()
	store.getProject = &models.Project{BaseEntity: models.BaseEntity{ID: "p1"}}
	store.listResult = models.PaginatedResult[models.Task]{
		Data:    []models.Task{{BaseEntity: models.BaseEntity{ID: "t1"}}},
		Total:   12,
		HasNext: true,
	}
	svc := services.NewTaskService(full, &store.stubBoard)

	page, err := svc.ListByProject(context.Background(), "p1", models.TaskFilter{IncludeHealing: false})
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if store.listFilter.IncludeHealing {
		t.Fatal("expected IncludeHealing=false on board filter")
	}
	if page.Total != 12 || !page.HasNext {
		t.Fatalf("pagination = total %d hasNext %v, want 12 true", page.Total, page.HasNext)
	}
}

type spyTaskBus struct {
	assigned int
	split    int
	retried  int
}

func (b *spyTaskBus) PublishTaskAssigned(context.Context, models.Task) { b.assigned++ }
func (b *spyTaskBus) PublishTaskSplit(context.Context, models.Task, []models.Task) {
	b.split++
}
func (b *spyTaskBus) PublishTaskRetried(context.Context, models.Task) { b.retried++ }

func TestTaskService_WithBus(t *testing.T) {
	_, full := newStore()
	svc := services.NewTaskService(full, nil)
	bus := &spyTaskBus{}
	withBus := svc.WithBus(bus)
	if withBus.Bus != bus {
		t.Fatal("WithBus should attach bus to copy")
	}
	if svc.Bus != nil {
		t.Fatal("original service should remain without bus")
	}
}

func TestTaskService_AssignAgent(t *testing.T) {
	store, full := newStore()
	now := time.Now().UTC()
	store.getTask = &models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: now},
		AgentID:    "old",
		State:      models.TaskStateReady,
	}
	bus := &spyTaskBus{}
	svc := services.NewTaskService(full, nil).WithBus(bus)

	updated, err := svc.AssignAgent(context.Background(), "task-1", "new-agent")
	if err != nil {
		t.Fatalf("AssignAgent: %v", err)
	}
	if updated.AgentID != "new-agent" {
		t.Fatalf("agent = %q, want new-agent", updated.AgentID)
	}
	if bus.assigned != 1 {
		t.Fatalf("assigned publishes = %d, want 1", bus.assigned)
	}
}

func TestTaskService_Split(t *testing.T) {
	store, full := newStore()
	now := time.Now().UTC()
	store.getTask = &models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: now},
		State:      models.TaskStateRunning,
	}
	bus := &spyTaskBus{}
	svc := services.NewTaskService(full, nil).WithBus(bus)

	parent, children, err := svc.Split(context.Background(), "task-1", []models.DraftTask{
		{Title: "a", Description: "do a"},
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if parent.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", parent.State)
	}
	if len(children) != 1 || children[0].Title != "a" {
		t.Fatalf("children = %#v", children)
	}
	if bus.split != 1 {
		t.Fatalf("split publishes = %d, want 1", bus.split)
	}
}

func TestTaskService_Retry(t *testing.T) {
	store, full := newStore()
	now := time.Now().UTC()
	store.getTask = &models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: now},
		State:      models.TaskStateRunning,
	}
	svc := services.NewTaskService(full, nil)
	if _, err := svc.Retry(context.Background(), "task-1"); !errors.Is(err, models.ErrInvalidStateTransition) {
		t.Fatalf("Retry from RUNNING err = %v, want ErrInvalidStateTransition", err)
	}

	store.getTask.State = models.TaskStateFailed
	bus := &spyTaskBus{}
	svc = services.NewTaskService(full, nil).WithBus(bus)
	updated, err := svc.Retry(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if updated.State != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", updated.State)
	}
	if bus.retried != 1 {
		t.Fatalf("retried publishes = %d, want 1", bus.retried)
	}
}
