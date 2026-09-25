package frontdesk

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agentd/internal/models"
)

// StatusSummarizer queries the KanbanStore and produces a deterministic
// status report. It makes no LLM calls.
type StatusSummarizer struct {
	store models.KanbanStore
}

func NewStatusSummarizer(store models.KanbanStore) *StatusSummarizer {
	return &StatusSummarizer{store: store}
}

// StatusReport is the structured response returned on status_check intent.
type StatusReport struct {
	Kind      string          `json:"kind"`
	Message   string          `json:"message"`
	Summary   StatusSummary   `json:"summary"`
	Attention []AttentionItem `json:"attention,omitempty"`
}

type AttentionItem struct {
	ProjectID      string              `json:"project_id"`
	ProjectName    string              `json:"project_name"`
	TaskID         string              `json:"task_id"`
	TaskTitle      string              `json:"task_title"`
	State          models.TaskState    `json:"state"`
	Assignee       models.TaskAssignee `json:"assignee"`
	RequiredAction string              `json:"required_action"`
	Explanation    string              `json:"explanation"`
}

type StatusSummary struct {
	TotalProjects int            `json:"total_projects"`
	TasksByState  map[string]int `json:"tasks_by_state"`
}

// SummarizeOptions controls filtering for status summarization.
type SummarizeOptions struct {
	IncludeHealing bool
	IncludeSystem  bool
}

func (s *StatusSummarizer) Summarize(ctx context.Context) (*StatusReport, error) {
	return s.SummarizeWithOptions(ctx, SummarizeOptions{IncludeHealing: true, IncludeSystem: true})
}

func (s *StatusSummarizer) SummarizeWithOptions(ctx context.Context, opts SummarizeOptions) (*StatusReport, error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	if !opts.IncludeSystem {
		filtered := projects[:0]
		for _, p := range projects {
			if p.Name != "_system" {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}
	if len(projects) == 0 {
		return &StatusReport{
			Kind:    "status_report",
			Message: "No active projects. Send a plan request to get started.",
			Summary: StatusSummary{TotalProjects: 0, TasksByState: map[string]int{}},
		}, nil
	}

	byState, attention, err := s.collectTaskStatus(ctx, projects, opts)
	if err != nil {
		return nil, err
	}

	remaining := 0
	for state, count := range byState {
		if state != string(models.TaskStateCompleted) && state != string(models.TaskStateFailed) {
			remaining += count
		}
	}

	message := buildMessage(len(projects), remaining, byState)
	if len(attention) > 0 {
		message += fmt.Sprintf(" %d item(s) need attention; open the Board to review the required action.", len(attention))
	}
	sortAttention(attention)
	return &StatusReport{
		Kind:      "status_report",
		Message:   message,
		Summary:   StatusSummary{TotalProjects: len(projects), TasksByState: byState},
		Attention: attention,
	}, nil
}

func (s *StatusSummarizer) collectTaskStatus(ctx context.Context, projects []models.Project, opts SummarizeOptions) (map[string]int, []AttentionItem, error) {
	byState := map[string]int{}
	var attention []AttentionItem
	for _, project := range projects {
		tasks, err := s.store.ListTasksByProject(ctx, project.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, task := range tasks {
			if !opts.IncludeHealing && models.IsSelfHealingHandoffTask(task) {
				continue
			}
			byState[string(task.State)]++
			if item, ok := buildAttentionItem(project, task); ok {
				attention = append(attention, item)
			}
		}
	}
	return byState, attention, nil
}

func sortAttention(items []AttentionItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].ProjectName == items[j].ProjectName {
			return items[i].TaskID < items[j].TaskID
		}
		return items[i].ProjectName < items[j].ProjectName
	})
}

func buildAttentionItem(project models.Project, task models.Task) (AttentionItem, bool) {
	action := ""
	explanation := ""
	if task.State == models.TaskStateCompleted || task.State == models.TaskStateFailed {
		return AttentionItem{}, false
	}
	switch {
	case task.Assignee == models.TaskAssigneeHuman:
		action = "Open the task and submit the required human result or action."
		explanation = "The task is assigned to a human and remains open."
	case task.State == models.TaskStateBlocked:
		action = "Open the blocked parent and resolve its open human handoff."
		explanation = "The task is blocked and waiting for a child handoff to resolve."
	case task.State == models.TaskStateFailedRequiresHuman:
		action = "Open the task and submit the required human result or action."
		explanation = "The task failed and requires human intervention."
	case task.State == models.TaskStateInConsideration:
		action = "Review the task and record the next decision in its comments."
		explanation = "The task is paused in human consideration."
	case task.State == models.TaskStateNeedsContext:
		action = "Re-gather or update the task context before continuing."
		explanation = "The task needs a fresh context pack."
	default:
		return AttentionItem{}, false
	}
	return AttentionItem{
		ProjectID:      project.ID,
		ProjectName:    project.Name,
		TaskID:         task.ID,
		TaskTitle:      task.Title,
		State:          task.State,
		Assignee:       task.Assignee,
		RequiredAction: action,
		Explanation:    explanation,
	}, true
}

func buildMessage(projectCount, remaining int, byState map[string]int) string {
	msg := fmt.Sprintf("You have %d active project(s) with %d task(s) remaining", projectCount, remaining)

	var details []string
	if n := byState[string(models.TaskStateRunning)]; n > 0 {
		details = append(details, fmt.Sprintf("%d running", n))
	}
	if n := byState[string(models.TaskStateReady)]; n > 0 {
		details = append(details, fmt.Sprintf("%d ready", n))
	}
	if n := byState[string(models.TaskStatePending)]; n > 0 {
		details = append(details, fmt.Sprintf("%d pending", n))
	}
	if len(details) > 0 {
		msg += " (" + strings.Join(details, ", ") + ")"
	}
	msg += ". Use the REST API or Board for full details."
	return msg
}
