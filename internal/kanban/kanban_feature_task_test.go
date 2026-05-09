package kanban

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"agentd/internal/models"
)

func (s *kanbanScenario) databaseHasOneReadyTask() error {
	_, err := s.seedTask("Ready", models.TaskStateReady, nil)
	return err
}

func (s *kanbanScenario) databaseHasOnePendingTask() error {
	_, err := s.seedTask("Pending", models.TaskStatePending, nil)
	return err
}

func (s *kanbanScenario) databaseHasOneCompletedTask() error {
	_, err := s.seedTask("Completed", models.TaskStateCompleted, nil)
	return err
}

func (s *kanbanScenario) queueCallsClaimNextReadyTasks(limitText string) error {
	limit, err := strconv.Atoi(limitText)
	if err != nil {
		return fmt.Errorf("parse claim limit: %w", err)
	}
	s.claimed, s.lastErr = s.store.ClaimNextReadyTasks(context.Background(), limit)
	return nil
}

func (s *kanbanScenario) onlyOneQueuedTaskShouldBeReturned() error {
	if s.lastErr != nil {
		return fmt.Errorf("claim ready tasks: %w", s.lastErr)
	}
	if len(s.claimed) != 1 {
		return fmt.Errorf("claimed %d tasks, want 1", len(s.claimed))
	}
	if s.claimed[0].State != models.TaskStateQueued {
		return fmt.Errorf("claimed task state = %s, want QUEUED", s.claimed[0].State)
	}
	return nil
}

func (s *kanbanScenario) claimedTaskStateShouldBeQueued() error {
	if len(s.claimed) != 1 {
		return fmt.Errorf("claimed %d tasks, want 1", len(s.claimed))
	}
	return s.taskShouldHaveState(s.claimed[0].Title, models.TaskStateQueued)
}

func (s *kanbanScenario) taskIsInState(title, state string) error {
	task, err := s.seedTask(title, models.TaskState(state), nil)
	if err != nil {
		return err
	}
	s.lastTaskTitle = task.Title
	return nil
}

func (s *kanbanScenario) taskIsInStateAndDependsOnTask(title, state, parentTitle string) error {
	child, err := s.seedTask(title, models.TaskState(state), nil)
	if err != nil {
		return err
	}
	parent, err := s.reloadTask(parentTitle)
	if err != nil {
		return err
	}
	if err := insertTaskRelation(context.Background(), s.store.db, parent.ID, child.ID); err != nil {
		return err
	}
	s.lastTaskTitle = child.Title
	return nil
}

func (s *kanbanScenario) workerCallsUpdateTaskResultSuccess(title string) error {
	task, err := s.reloadTask(title)
	if err != nil {
		return err
	}
	updated, err := s.store.UpdateTaskResult(
		context.Background(),
		task.ID,
		task.UpdatedAt,
		models.TaskResult{Success: true},
	)
	s.lastErr = err
	if err != nil {
		return nil
	}
	s.tasks[updated.Title] = *updated
	s.lastTaskTitle = updated.Title
	return nil
}

func (s *kanbanScenario) taskShouldMoveTo(title, state string) error {
	return s.taskShouldHaveState(title, models.TaskState(state))
}

func (s *kanbanScenario) taskShouldMoveFromTo(title, _, next string) error {
	return s.taskShouldHaveState(title, models.TaskState(next))
}

func (s *kanbanScenario) humanAddsACommentToTask(title string) error {
	task, err := s.reloadTask(title)
	if err != nil {
		return err
	}
	s.tasks[task.Title] = task
	s.lastTaskTitle = task.Title
	return s.store.AddComment(context.Background(), models.Comment{
		TaskID: task.ID,
		Author: "Human",
		Body:   "please review before completing",
	})
}

func (s *kanbanScenario) lastTaskStateShouldChangeTo(state string) error {
	return s.taskShouldHaveState(s.lastTaskTitle, models.TaskState(state))
}

func (s *kanbanScenario) agentSubsequentlyTriesUpdateTaskResult(title string) error {
	task, ok := s.tasks[normalizeTaskTitle(title)]
	if !ok {
		return fmt.Errorf("task %q was not observed before conflict", title)
	}
	_, s.lastErr = s.store.UpdateTaskResult(
		context.Background(),
		task.ID,
		task.UpdatedAt,
		models.TaskResult{Success: true},
	)
	s.lastTaskTitle = task.Title
	return nil
}

func (s *kanbanScenario) databaseUpdateShouldFailWithStateConflict() error {
	if !errors.Is(s.lastErr, models.ErrStateConflict) {
		return fmt.Errorf("update error = %v, want ErrStateConflict", s.lastErr)
	}
	return nil
}

func (s *kanbanScenario) lastTaskStateShouldRemain(state string) error {
	return s.taskShouldHaveState(s.lastTaskTitle, models.TaskState(state))
}
