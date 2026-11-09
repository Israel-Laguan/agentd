package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	agenthooks "agentd/internal/agent/hooks"
	"agentd/internal/gateway"
	"agentd/internal/gateway/truncation"
	"agentd/internal/models"
	"agentd/internal/stage"
)

const attemptHandoffEvent = "ATTEMPT_HANDOFF"

// errAttemptHandoffUnavailable marks a retry that cannot see the last note. It
// is retried as a failure and never replaces the note it could not read.
var errAttemptHandoffUnavailable = errors.New("attempt handoff unavailable")

// attemptHandoff contains only facts available to the controller. Agent text
// may describe decisions, files and remaining work; it is not inferred.
type attemptHandoff struct {
	Goal          string   `json:"goal"`
	TaskDetails   string   `json:"task_details_verbatim"`
	Criteria      []string `json:"criteria_verbatim,omitempty"`
	OriginalInput string   `json:"original_user_request_verbatim,omitempty"`
	Progress      string   `json:"agent_progress,omitempty"`
	Failure       string   `json:"failure,omitempty"`
	FailedTools   []string `json:"failed_tools,omitempty"`
	Files         []string `json:"files_touched,omitempty"`
	Plan          string   `json:"agent_plan,omitempty"`
}

func (w *Worker) writeAttemptHandoff(ctx context.Context, task models.Task, project models.Project, result stage.StageResult, failure error) error {
	if w.store == nil {
		return errors.New("attempt handoff store unavailable")
	}
	note := attemptHandoff{Goal: task.Title, TaskDetails: task.Description, Criteria: task.SuccessCriteria, OriginalInput: project.OriginalInput, Progress: result.Progress, FailedTools: append([]string(nil), result.FailedTools...), Files: result.FilesTouched, Plan: result.Plan}
	if failure != nil {
		note.Failure = failure.Error()
	}
	if w.sandboxScrubber != nil {
		note.Progress = w.sandboxScrubber.Scrub(note.Progress)
		note.Failure = w.sandboxScrubber.Scrub(note.Failure)
		note.Plan = w.sandboxScrubber.Scrub(note.Plan)
		for i := range note.FailedTools {
			note.FailedTools[i] = w.sandboxScrubber.Scrub(note.FailedTools[i])
		}
	}
	cut := truncation.HeadTailStrategy{HeadRatio: .5}
	note.Progress = cut.Truncate(note.Progress, 8192)
	note.Failure = cut.Truncate(note.Failure, 8192)
	note.Plan = cut.Truncate(note.Plan, 8192)
	for i := range note.FailedTools {
		note.FailedTools[i] = cut.Truncate(note.FailedTools[i], 8192)
	}
	payload, err := json.Marshal(note)
	if err == nil {
		liveCtx, cancel := liveContext(ctx)
		defer cancel()
		err = w.store.AppendEvent(liveCtx, models.Event{ProjectID: task.ProjectID, TaskID: sql.NullString{String: task.ID, Valid: true}, Type: attemptHandoffEvent, Payload: string(payload)})
	}
	return err
}

// attemptHandoffMessage returns the latest uncleared handoff note, or nil. A
// note that cannot be read is an error: a retry must not run without it.
func (w *Worker) attemptHandoffMessage(ctx context.Context, task models.Task) (*gateway.PromptMessage, error) {
	if w.store == nil {
		return nil, nil
	}
	events, err := w.store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: load: %w", errAttemptHandoffUnavailable, err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != attemptHandoffEvent {
			continue
		}
		if events[i].Payload == "" {
			return nil, nil
		}
		var note attemptHandoff
		if err := json.Unmarshal([]byte(events[i].Payload), &note); err != nil {
			return nil, fmt.Errorf("%w: invalid note: %w", errAttemptHandoffUnavailable, err)
		}
		// User text stays verbatim; only the agent's observations are wrapped
		// as external, untrusted content.
		userText := "Previous failed attempt handoff\nGoal: " + note.Goal + "\nTask details (verbatim):\n" + note.TaskDetails
		for _, criterion := range note.Criteria {
			userText += "\nSuccess criterion (verbatim): " + criterion
		}
		if note.OriginalInput != "" {
			userText += "\nOriginal user request (verbatim):\n" + note.OriginalInput
		}
		note.Goal, note.TaskDetails, note.OriginalInput, note.Criteria = "", "", "", nil
		observations, _ := json.Marshal(note)
		return &gateway.PromptMessage{Role: "user", Name: "attempt_handoff", Section: gateway.SectionComments,
			Content: userText + "\nAgent progress is reported, not verified:\n" + agenthooks.WrapExternalContent("attempt handoff", string(observations))}, nil
	}
	return nil, nil
}

func (w *Worker) clearAttemptHandoff(ctx context.Context, task models.Task) {
	if w.store == nil {
		return
	}
	liveCtx, cancel := liveContext(ctx)
	defer cancel()
	events, err := w.store.ListEventsByTask(liveCtx, task.ID)
	if err != nil {
		slog.Error("failed to inspect attempt handoff", "task_id", task.ID, "error", err)
		return
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == attemptHandoffEvent {
			if events[i].Payload != "" {
				err = w.store.AppendEvent(liveCtx, models.Event{ProjectID: task.ProjectID, TaskID: sql.NullString{String: task.ID, Valid: true}, Type: attemptHandoffEvent})
				if err != nil {
					slog.Error("failed to clear attempt handoff", "task_id", task.ID, "error", err)
				}
			}
			return
		}
	}
}
