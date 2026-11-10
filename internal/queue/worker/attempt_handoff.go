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

// Caps keep the protected note well inside acpContextBudget: at most
// 3*4 KiB of text, 4*2 KiB of failed calls, 50 paths and 8 KiB of user request.
const (
	handoffTextCap    = 4096
	handoffToolCap    = 2048
	handoffMaxTools   = 4
	handoffMaxFiles   = 50
	handoffPathCap    = 256
	handoffRequestCap = 8192
)

// attemptHandoff contains only facts available to the controller. Agent text
// may describe decisions, files and remaining work; it is not inferred. The
// task and the user's request are read live when the note is shown, so the
// event log never holds a copy of them.
type attemptHandoff struct {
	Progress    string   `json:"agent_progress,omitempty"`
	Failure     string   `json:"failure,omitempty"`
	FailedTools []string `json:"failed_tools,omitempty"`
	Files       []string `json:"files_touched,omitempty"`
	Plan        string   `json:"agent_plan,omitempty"`
}

func (w *Worker) writeAttemptHandoff(ctx context.Context, task models.Task, result stage.StageResult, failure error) error {
	if w.store == nil {
		return errors.New("attempt handoff store unavailable")
	}
	note := attemptHandoff{Progress: result.Progress, FailedTools: append([]string(nil), result.FailedTools...), Files: append([]string(nil), result.FilesTouched...), Plan: result.Plan}
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
	note.Progress = cut.Truncate(note.Progress, handoffTextCap)
	note.Failure = cut.Truncate(note.Failure, handoffTextCap)
	note.Plan = cut.Truncate(note.Plan, handoffTextCap)
	// The first failures are kept: later ones usually follow from them.
	note.FailedTools = capList(note.FailedTools, handoffMaxTools, "failed calls")
	for i := range note.FailedTools {
		note.FailedTools[i] = cut.Truncate(note.FailedTools[i], handoffToolCap)
	}
	note.Files = capList(note.Files, handoffMaxFiles, "files")
	for i := range note.Files {
		note.Files[i] = cut.Truncate(note.Files[i], handoffPathCap)
	}
	payload, err := json.Marshal(note)
	if err == nil {
		liveCtx, cancel := liveContext(ctx)
		defer cancel()
		err = w.store.AppendEvent(liveCtx, models.Event{ProjectID: task.ProjectID, TaskID: sql.NullString{String: task.ID, Valid: true}, Type: attemptHandoffEvent, Payload: string(payload)})
	}
	return err
}

// capList keeps the first max entries and notes how many were dropped.
func capList(list []string, max int, what string) []string {
	if len(list) <= max {
		return list
	}
	return append(list[:max:max], fmt.Sprintf("%d more %s omitted", len(list)-max, what)) // never writes into the caller's array
}

// attemptHandoffMessage returns the latest uncleared handoff note, or nil. A
// note that cannot be read is an error: a retry must not run without it.
func (w *Worker) attemptHandoffMessage(ctx context.Context, task models.Task, project models.Project) (*gateway.PromptMessage, error) {
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
		return &gateway.PromptMessage{Role: "user", Name: "attempt_handoff", Section: gateway.SectionComments,
			Content: renderAttemptHandoff(note, project.OriginalInput)}, nil
	}
	return nil, nil
}

// keepsOlderHandoff reports whether a pre-start failure should leave the
// newest handoff event alone: it holds a note (a success writes an empty one
// that clears it), or it cannot be read. The read uses a live context, since the
// failed call's own may be done, and a read error keeps the old note rather than
// risk shadowing it.
func (w *Worker) keepsOlderHandoff(ctx context.Context, task models.Task) bool {
	if w.store == nil {
		return false
	}
	liveCtx, cancel := liveContext(ctx)
	defer cancel()
	events, err := w.store.ListEventsByTask(liveCtx, task.ID)
	if err != nil {
		return true
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == attemptHandoffEvent {
			return events[i].Payload != ""
		}
	}
	return false
}

// handoffRenderedMax bounds the message as the prompt carries it, 3/4 of the
// budget so the system and task messages still fit (B-096). The caps at write
// time count characters; JSON and the external-content wrapper escape <, > & and
// quotes to several bytes each, so the rendered size is what must be bounded.
const handoffRenderedMax = acpContextBudget * 3 / 4

// renderAttemptHandoff builds the handoff message text. The task is already in
// the protected task message. The user's request stays verbatim up to its cap;
// only the agent's observations are wrapped as external, untrusted content. If
// the escaped result is over handoffRenderedMax, every field's cap is halved
// until it fits: the head and tail of each field survive, never a whole field.
func renderAttemptHandoff(note attemptHandoff, original string) string {
	cut := truncation.HeadTailStrategy{HeadRatio: .5}
	for limit := handoffTextCap; ; limit /= 2 {
		text := "Previous failed attempt handoff for the task above."
		if original != "" {
			text += "\nOriginal user request (verbatim):\n" + cut.Truncate(original, min(handoffRequestCap, limit*2))
		}
		observations, _ := json.Marshal(shrinkAttemptHandoff(note, limit))
		out := text + "\nAgent progress is reported, not verified:\n" + agenthooks.WrapExternalContent("attempt handoff", string(observations))
		if len(out) <= handoffRenderedMax || limit <= 64 {
			return out
		}
	}
}

// shrinkAttemptHandoff cuts each text field to limit characters, tools to half
// of it and paths to a sixteenth. At limit == handoffTextCap it changes nothing
// the writer's own caps allow.
func shrinkAttemptHandoff(n attemptHandoff, limit int) attemptHandoff {
	cut := truncation.HeadTailStrategy{HeadRatio: .5}
	out := n
	out.Progress, out.Failure, out.Plan = cut.Truncate(n.Progress, limit), cut.Truncate(n.Failure, limit), cut.Truncate(n.Plan, limit)
	out.FailedTools = make([]string, len(n.FailedTools))
	for i, tool := range n.FailedTools {
		out.FailedTools[i] = cut.Truncate(tool, max(limit/2, 1))
	}
	out.Files = make([]string, len(n.Files))
	for i, f := range n.Files {
		out.Files[i] = cut.Truncate(f, max(limit/16, 16))
	}
	return out
}

// clearAttemptHandoff runs after the coordinator committed a success, which it
// cannot undo, so a failure is logged. A note left behind is only shown again
// if the task is reopened, where the last failure is still useful history.
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
