package frontdesk

import (
	"context"
	"fmt"
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
	Kind    string        `json:"kind"`
	Message string        `json:"message"`
	Summary StatusSummary `json:"summary"`
}

type StatusSummary struct {
	TotalProjects int            `json:"total_projects"`
	TasksByState  map[string]int `json:"tasks_by_state"`
}

func (s *StatusSummarizer) Summarize(ctx context.Context) (*StatusReport, error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return &StatusReport{
			Kind:    "status_report",
			Message: "No active projects. Send a plan request to get started.",
			Summary: StatusSummary{TotalProjects: 0, TasksByState: map[string]int{}},
		}, nil
	}

	byState := map[string]int{}
	for _, p := range projects {
		tasks, err := s.store.ListTasksByProject(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			byState[string(t.State)]++
		}
	}

	remaining := 0
	for state, count := range byState {
		if state != string(models.TaskStateCompleted) && state != string(models.TaskStateFailed) {
			remaining += count
		}
	}

	return &StatusReport{
		Kind:    "status_report",
		Message: buildMessage(len(projects), remaining, byState),
		Summary: StatusSummary{TotalProjects: len(projects), TasksByState: byState},
	}, nil
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
