package services

import (
	"context"
	"strings"

	"agentd/internal/models"
)

// TaskService coordinates human-driven task operations: comments, state
// patches, and paginated listings. State guards stay in the store layer
// (see internal/kanban), so this service is intentionally thin.
type TaskService struct {
	Store models.KanbanStore
	Board models.KanbanBoardContract
	Bus   TaskBus
}

// TaskBus is the optional bus surface used to publish manager-loop
// signals (assign / split / retry). Implementations MUST be non-blocking;
// the durable record of these actions lives in the events table.
type TaskBus interface {
	PublishTaskAssigned(ctx context.Context, task models.Task)
	PublishTaskSplit(ctx context.Context, parent models.Task, children []models.Task)
	PublishTaskRetried(ctx context.Context, task models.Task)
}

// NewTaskService wires the persistence boundary used by task controllers.
// Board may be nil; in that case state-pause behavior falls through to
// the store's plain AddComment path.
func NewTaskService(store models.KanbanStore, board models.KanbanBoardContract) *TaskService {
	return &TaskService{Store: store, Board: board}
}

// WithBus returns a shallow copy of the service that publishes manager
// loop signals through the given bus implementation.
func (s *TaskService) WithBus(taskBus TaskBus) *TaskService {
	cp := *s
	cp.Bus = taskBus
	return &cp
}

// AddHumanComment records a USER comment on a task. It does NOT pre-check
// the task state in Go; the store transaction is responsible for either
// pausing the task into IN_CONSIDERATION or rejecting the write with a
// state-conflict sentinel. After commit, the store invokes the registered
// TaskCanceller to terminate any running worker for the task.
func (s *TaskService) AddHumanComment(ctx context.Context, taskID, content string) (models.Comment, error) {
	body := strings.TrimSpace(content)
	if body == "" {
		return models.Comment{}, models.ErrInvalidDraftPlan
	}
	comment := models.Comment{
		TaskID:  taskID,
		Author:  models.CommentAuthorUser,
		Body:    body,
		Content: body,
	}
	if s.Board != nil {
		if err := s.Board.AddCommentAndPause(ctx, taskID, comment); err != nil {
			return models.Comment{}, err
		}
		return comment, nil
	}
	if err := s.Store.AddComment(ctx, comment); err != nil {
		return models.Comment{}, err
	}
	return comment, nil
}

// UpdateTaskState transitions a task to the requested state using the
// store's optimistic-locked update. The current task is fetched only to
// supply the expected updated_at timestamp; the SQL WHERE clause inside
// UpdateTaskState rejects the write if another writer raced ahead, in
// which case ErrStateConflict propagates back to the controller.
func (s *TaskService) UpdateTaskState(ctx context.Context, taskID string, next models.TaskState) (*models.Task, error) {
	if !next.Valid() {
		return nil, models.ErrInvalidStateTransition
	}
	current, err := s.Store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return s.Store.UpdateTaskState(ctx, taskID, current.UpdatedAt, next)
}

// ListByProject returns a paginated slice of tasks for a project, applying
// the filter (state, assignee) supplied by the caller. The project is
// looked up first so callers receive ErrProjectNotFound rather than an
// empty page when the id is wrong.
func (s *TaskService) ListByProject(
	ctx context.Context,
	projectID string,
	filter models.TaskFilter,
) (models.PaginatedResult[models.Task], error) {
	if _, err := s.Store.GetProject(ctx, projectID); err != nil {
		return models.PaginatedResult[models.Task]{}, err
	}
	pid := strings.TrimSpace(projectID)
	filter.ProjectID = &pid
	if s.Board != nil {
		return s.Board.ListTasks(ctx, filter)
	}
	tasks, err := s.Store.ListTasksByProject(ctx, projectID)
	if err != nil {
		return models.PaginatedResult[models.Task]{}, err
	}
	return models.PaginatedResult[models.Task]{Data: tasks, Total: len(tasks), HasNext: false}, nil
}

// AssignAgent retargets a task to a different agent profile. The store
// validates the agent exists and rejects retargeting a RUNNING task with
// ErrStateConflict; pause via comments first if a live swap is intended.
func (s *TaskService) AssignAgent(ctx context.Context, taskID, agentID string) (*models.Task, error) {
	current, err := s.Store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	updated, err := s.Store.AssignTaskAgent(ctx, taskID, current.UpdatedAt, strings.TrimSpace(agentID))
	if err != nil {
		return nil, err
	}
	if s.Bus != nil {
		s.Bus.PublishTaskAssigned(ctx, *updated)
	}
	return updated, nil
}

// Split blocks the parent and creates ready subtasks. Wraps
// Store.BlockTaskWithSubtasks so the same SQL transaction guards used by
// worker self-breakdown are reused here for human-driven splits.
func (s *TaskService) Split(ctx context.Context, taskID string, subtasks []models.DraftTask) (*models.Task, []models.Task, error) {
	current, err := s.Store.GetTask(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	parent, children, err := s.Store.BlockTaskWithSubtasks(ctx, taskID, current.UpdatedAt, subtasks)
	if err != nil {
		return nil, nil, err
	}
	if s.Bus != nil {
		s.Bus.PublishTaskSplit(ctx, *parent, children)
	}
	return parent, children, nil
}

// Retry transitions a stuck task back to READY so the dispatcher can pick
// it up again, picking up the current agent_id and any new comments left
// since the previous attempt.
func (s *TaskService) Retry(ctx context.Context, taskID string) (*models.Task, error) {
	current, err := s.Store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	switch current.State {
	case models.TaskStateFailed, models.TaskStateFailedRequiresHuman, models.TaskStateBlocked, models.TaskStateInConsideration:
	default:
		return nil, models.ErrInvalidStateTransition
	}
	updated, err := s.Store.UpdateTaskState(ctx, taskID, current.UpdatedAt, models.TaskStateReady)
	if err != nil {
		return nil, err
	}
	if s.Bus != nil {
		s.Bus.PublishTaskRetried(ctx, *updated)
	}
	return updated, nil
}
