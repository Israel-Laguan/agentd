package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/services"
)

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
