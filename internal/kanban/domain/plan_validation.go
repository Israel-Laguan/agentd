package domain

import (
	"fmt"
	"strings"

	"agentd/internal/models"
)

// NormalizeDraftPlan trims and validates draft plan invariants in-place on the task slice.
func NormalizeDraftPlan(plan models.DraftPlan) (models.DraftPlan, error) {
	if err := validateDraftHeader(plan); err != nil {
		return plan, err
	}
	seen, err := normalizeDraftTasks(plan.Tasks)
	if err != nil {
		return plan, err
	}
	if err := validateDraftDependencies(plan.Tasks, seen); err != nil {
		return plan, err
	}
	return plan, nil
}

func validateDraftHeader(plan models.DraftPlan) error {
	if strings.TrimSpace(plan.ProjectName) == "" {
		return fmt.Errorf("%w: project name is required", models.ErrInvalidDraftPlan)
	}
	if len(plan.Tasks) == 0 {
		return fmt.Errorf("%w: at least one task is required", models.ErrInvalidDraftPlan)
	}
	return nil
}

// ValidateTaskCap ensures the draft does not exceed maxTasks when maxTasks > 0.
func ValidateTaskCap(plan models.DraftPlan, maxTasks int) error {
	if maxTasks <= 0 || len(plan.Tasks) <= maxTasks {
		return nil
	}
	return fmt.Errorf("%w: draft plan has %d tasks, max is %d", models.ErrInvalidDraftPlan, len(plan.Tasks), maxTasks)
}

func normalizeDraftTasks(tasks []models.DraftTask) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(tasks))
	for i := range tasks {
		if err := normalizeDraftTask(&tasks[i], i, seen); err != nil {
			return nil, err
		}
	}
	return seen, nil
}

func normalizeDraftTask(task *models.DraftTask, index int, seen map[string]struct{}) error {
	task.ReferenceID = strings.TrimSpace(task.ReferenceID)
	task.TempID = strings.TrimSpace(task.TempID)
	task.Title = strings.TrimSpace(task.Title)
	if task.ReferenceID == "" && task.TempID == "" {
		task.ReferenceID = fmt.Sprintf("task-%d", index+1)
		task.TempID = task.ReferenceID
	}
	if task.ReferenceID == "" {
		task.ReferenceID = task.TempID
	}
	if task.TempID == "" {
		task.TempID = task.ReferenceID
	}
	taskID := task.ID()
	if task.Title == "" {
		return fmt.Errorf("%w: task %q title is required", models.ErrInvalidDraftPlan, taskID)
	}
	if _, ok := seen[taskID]; ok {
		return fmt.Errorf("%w: duplicate task id %q", models.ErrInvalidDraftPlan, taskID)
	}
	seen[taskID] = struct{}{}
	return normalizeDraftAssignee(task)
}

func normalizeDraftAssignee(task *models.DraftTask) error {
	if task.Assignee == "" {
		task.Assignee = models.TaskAssigneeSystem
	}
	if !task.Assignee.Valid() {
		return fmt.Errorf("%w: invalid assignee %q", models.ErrInvalidDraftPlan, task.Assignee)
	}
	return nil
}

func validateDraftDependencies(tasks []models.DraftTask, seen map[string]struct{}) error {
	for _, task := range tasks {
		for _, parentID := range task.DependsOn {
			if _, ok := seen[parentID]; !ok {
				return fmt.Errorf("%w: task %q depends on unknown task %q", models.ErrInvalidDraftPlan, task.ID(), parentID)
			}
		}
	}
	return nil
}
