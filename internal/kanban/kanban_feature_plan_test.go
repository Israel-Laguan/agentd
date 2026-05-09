package kanban

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"agentd/internal/models"
)

func (s *kanbanScenario) aDraftPlanWithTasks(titlesText string) error {
	titles := parseTaskTitles(titlesText)
	if len(titles) == 0 {
		return fmt.Errorf("expected at least one task title in %q", titlesText)
	}

	s.plan = models.DraftPlan{
		ProjectName: "behavior plan",
		Description: "gherkin task dependency scenario",
		Tasks:       make([]models.DraftTask, 0, len(titles)),
	}
	for _, title := range titles {
		s.plan.Tasks = append(s.plan.Tasks, models.DraftTask{
			TempID: strings.ToLower(title),
			Title:  title,
		})
	}
	return nil
}

func (s *kanbanScenario) iHaveValidDraftPlanWithTasks(countText, titlesText string) error {
	count, err := strconv.Atoi(countText)
	if err != nil {
		return fmt.Errorf("parse task count: %w", err)
	}
	titles := parseTaskTitles(titlesText)
	if len(titles) != count {
		return fmt.Errorf("got %d task titles, want %d", len(titles), count)
	}
	return s.aDraftPlanWithTasks(titlesText)
}

func (s *kanbanScenario) iHaveCircularDraftPlan() error {
	s.plan = models.DraftPlan{
		ProjectName: "circular plan",
		Description: "cycle rejection",
		Tasks: []models.DraftTask{
			{TempID: "a", Title: "A", DependsOn: []string{"b"}},
			{TempID: "b", Title: "B"},
		},
	}
	return nil
}

func (s *kanbanScenario) tasksDependOn(dependentsText, prerequisite string) error {
	for _, dependent := range parseTaskTitles(dependentsText) {
		if err := s.addDependency(dependent, prerequisite); err != nil {
			return err
		}
	}
	return nil
}

func (s *kanbanScenario) taskDependsOn(dependent, prerequisitesText string) error {
	dependent = normalizeTaskTitle(dependent)
	for _, prerequisite := range parseTaskTitles(prerequisitesText) {
		if err := s.addDependency(dependent, prerequisite); err != nil {
			return err
		}
	}
	return nil
}

func (s *kanbanScenario) addDependency(dependent, prerequisite string) error {
	dependent = normalizeTaskTitle(dependent)
	prerequisite = normalizeTaskTitle(prerequisite)
	for i := range s.plan.Tasks {
		if s.plan.Tasks[i].Title == dependent {
			s.plan.Tasks[i].DependsOn = append(s.plan.Tasks[i].DependsOn, strings.ToLower(prerequisite))
			return nil
		}
	}
	return fmt.Errorf("task %q not found in draft plan", dependent)
}

func (s *kanbanScenario) thePlanIsMaterialized() error {
	project, tasks, err := s.store.MaterializePlan(context.Background(), s.plan)
	if err != nil {
		return fmt.Errorf("materialize plan: %w", err)
	}
	s.project = project
	s.tasks = make(map[string]models.Task, len(tasks))
	for _, task := range tasks {
		s.tasks[task.Title] = task
	}
	return nil
}

func (s *kanbanScenario) iCallMaterializePlan() error {
	s.project, _, s.lastErr = s.store.MaterializePlan(context.Background(), s.plan)
	if s.lastErr != nil {
		return nil
	}
	return s.refreshProjectTasks()
}

func (s *kanbanScenario) oneProjectShouldBeCreated() error {
	return s.tableCountShouldBe("projects", 1)
}

func (s *kanbanScenario) tasksShouldBeCreated(countText string) error {
	count, err := strconv.Atoi(countText)
	if err != nil {
		return fmt.Errorf("parse task count: %w", err)
	}
	return s.tableCountShouldBe("tasks", count)
}

func (s *kanbanScenario) namedTaskShouldHaveState(title string, state string) error {
	return s.taskShouldHaveState(normalizeTaskTitle(title), models.TaskState(state))
}

func (s *kanbanScenario) twoNamedTasksShouldHaveState(first, second, state string) error {
	if err := s.taskShouldHaveState(normalizeTaskTitle(first), models.TaskState(state)); err != nil {
		return err
	}
	return s.taskShouldHaveState(normalizeTaskTitle(second), models.TaskState(state))
}

func (s *kanbanScenario) taskRelationsShouldHaveEntries(countText string) error {
	count, err := strconv.Atoi(countText)
	if err != nil {
		return fmt.Errorf("parse relation count: %w", err)
	}
	return s.tableCountShouldBe("task_relations", count)
}

func (s *kanbanScenario) dependencyEdgeTestedForCycles(fromTitle, toTitle string) error {
	from, err := s.reloadTask(fromTitle)
	if err != nil {
		return err
	}
	to, err := s.reloadTask(toTitle)
	if err != nil {
		return err
	}
	tx, txErr := beginImmediate(context.Background(), s.store.db)
	if txErr != nil {
		return txErr
	}
	defer rollbackUnlessCommitted(tx)
	s.lastErr = ensureNoCycle(context.Background(), tx, from.ID, to.ID)
	return nil
}

func (s *kanbanScenario) operationShouldReturnCircularDependencyError() error {
	if !errors.Is(s.lastErr, models.ErrCircularDependency) {
		return fmt.Errorf("operation error = %v, want ErrCircularDependency", s.lastErr)
	}
	return nil
}

func (s *kanbanScenario) noProjectOrTaskRecordsShouldBeWritten() error {
	if err := s.tableCountShouldBe("projects", 0); err != nil {
		return err
	}
	return s.tableCountShouldBe("tasks", 0)
}

func (s *kanbanScenario) tasksShouldBeReady(titlesText string) error {
	return s.tasksShouldHaveState(titlesText, models.TaskStateReady)
}

func (s *kanbanScenario) tasksShouldBePending(titlesText string) error {
	return s.tasksShouldHaveState(titlesText, models.TaskStatePending)
}

func (s *kanbanScenario) tasksShouldBeBlocked(titlesText string) error {
	return s.tasksShouldHaveState(titlesText, models.TaskStateBlocked)
}

func (s *kanbanScenario) taskIsBlockedWithSubtasks(parentTitle, subtasksText string) error {
	parent, err := s.reloadTask(parentTitle)
	if err != nil {
		return err
	}
	running, err := s.store.UpdateTaskState(context.Background(), parent.ID, parent.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		return fmt.Errorf("start parent task %q: %w", parentTitle, err)
	}
	drafts := make([]models.DraftTask, 0)
	for _, title := range parseTaskTitles(subtasksText) {
		drafts = append(drafts, models.DraftTask{Title: title})
	}
	blocked, children, err := s.store.BlockTaskWithSubtasks(context.Background(), running.ID, running.UpdatedAt, drafts)
	if err != nil {
		return fmt.Errorf("block task %q: %w", parentTitle, err)
	}
	s.tasks[blocked.Title] = *blocked
	for _, child := range children {
		s.tasks[child.Title] = child
	}
	return nil
}

func (s *kanbanScenario) tasksShouldHaveState(titlesText string, want models.TaskState) error {
	for _, title := range parseTaskTitles(titlesText) {
		if err := s.taskShouldHaveState(title, want); err != nil {
			return err
		}
	}
	return nil
}

func (s *kanbanScenario) taskShouldHaveState(title string, want models.TaskState) error {
	task, err := s.reloadTask(title)
	if err != nil {
		return err
	}
	if task.State != want {
		return fmt.Errorf("task %q state = %s, want %s", title, task.State, want)
	}
	return nil
}

func (s *kanbanScenario) taskIsCompleted(title string) error {
	task, ok := s.tasks[title]
	if !ok {
		return fmt.Errorf("task %q was not materialized", title)
	}
	running, err := s.store.UpdateTaskState(context.Background(), task.ID, task.UpdatedAt, models.TaskStateRunning)
	if err != nil {
		return fmt.Errorf("start task %q: %w", title, err)
	}
	completed, err := s.store.UpdateTaskResult(
		context.Background(),
		running.ID,
		running.UpdatedAt,
		models.TaskResult{Success: true},
	)
	if err != nil {
		return fmt.Errorf("complete task %q: %w", title, err)
	}
	s.tasks[title] = *completed
	return nil
}

func (s *kanbanScenario) tasksShouldBeClaimable(titlesText string) error {
	ready, err := s.store.ClaimNextReadyTasks(context.Background(), 10)
	if err != nil {
		return fmt.Errorf("claim ready tasks: %w", err)
	}
	readyByTitle := make(map[string]bool, len(ready))
	for _, task := range ready {
		readyByTitle[task.Title] = true
	}
	for _, title := range parseTaskTitles(titlesText) {
		if !readyByTitle[title] {
			return fmt.Errorf("expected task %q to be claimable, got %v", title, readyByTitle)
		}
	}
	return nil
}
