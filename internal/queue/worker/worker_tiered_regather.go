package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agentd/internal/models"
)

// tieredNeedsContextEvent records that a step declared its ContextPack
// insufficient and triggered a re-gather.
const tieredNeedsContextEvent = "TIERED_NEEDS_CONTEXT"

// needsContextSignal is the JSON a decision, execute, or verify step emits
// instead of its normal output when the sealed pack cannot answer the task.
type needsContextSignal struct {
	NeedsContext bool   `json:"needs_context"`
	Reason       string `json:"reason"`
}

// detectNeedsContext reports whether the step's committed output asked for a
// re-gather, along with the reason it gave.
func (w *Worker) detectNeedsContext(ctx context.Context, task models.Task) (string, bool) {
	payload, err := w.latestResultPayload(ctx, task)
	if err != nil {
		return "", false
	}
	return parseNeedsContext(payload)
}

// parseNeedsContext extracts a re-gather request from step output.
func parseNeedsContext(output string) (string, bool) {
	candidate := extractJSONObject(output)
	if candidate == "" {
		return "", false
	}
	var signal needsContextSignal
	if err := json.Unmarshal([]byte(candidate), &signal); err != nil {
		return "", false
	}
	if !signal.NeedsContext {
		return "", false
	}
	reason := strings.TrimSpace(signal.Reason)
	if reason == "" {
		reason = "step reported the ContextPack was insufficient"
	}
	return reason, true
}

// handleNeedsContext performs the re-gather as an explicit board action: the
// stale downstream steps are abandoned, and a fresh
// context -> decision -> execute -> verify chain is appended to the origin.
//
// Appending a new chain rather than rewiring the existing DEPENDS_ON edges is
// deliberate: the new steps depend on the new context step by construction, so
// no edge surgery (and no new store method) is needed, and the abandoned steps
// stay on the board as a record of what was discarded.
func (w *Worker) handleNeedsContext(ctx context.Context, task models.Task, originTask models.Task, reason string) error {
	slog.InfoContext(ctx, "tiered: re-gather requested",
		"task_id", task.ID, "origin_id", originTask.ID, "reason", reason)
	w.Emit(ctx, task, tieredNeedsContextEvent, reason)

	if err := w.abandonStaleTieredSteps(ctx, task, originTask); err != nil {
		return fmt.Errorf("abandon stale steps: %w", err)
	}
	if err := w.appendTieredRegather(ctx, originTask, reason); err != nil {
		return fmt.Errorf("append re-gather chain: %w", err)
	}
	w.Emit(ctx, originTask, "TIERED_REGATHER_SCHEDULED", reason)
	return nil
}

// abandonStaleTieredSteps parks every not-yet-terminal sibling step in
// NEEDS_CONTEXT so nothing downstream runs against the stale pack. Steps that
// are already RUNNING are left alone — per the spec they finish, but their
// output is superseded by the new chain.
func (w *Worker) abandonStaleTieredSteps(ctx context.Context, trigger models.Task, originTask models.Task) error {
	steps, err := w.tieredStepChildren(ctx, originTask)
	if err != nil {
		return err
	}
	for _, step := range steps {
		if step.ID == trigger.ID {
			continue
		}
		switch step.State {
		case models.TaskStatePending, models.TaskStateReady, models.TaskStateQueued:
		default:
			continue
		}
		if _, err := w.store.UpdateTaskState(ctx, step.ID, step.UpdatedAt, models.TaskStateNeedsContext); err != nil {
			slog.WarnContext(ctx, "tiered: failed to park stale step",
				"task_id", step.ID, "error", err)
			continue
		}
		w.Emit(ctx, step, tieredNeedsContextEvent, "superseded by re-gather")
	}
	return nil
}

// appendTieredRegather attaches a fresh full pipeline to the origin.
func (w *Worker) appendTieredRegather(ctx context.Context, originTask models.Task, reason string) error {
	origin, err := w.originForAppend(ctx, originTask.ID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	contextDescription := fmt.Sprintf(
		"Re-gather: a previous step reported the ContextPack was insufficient.\n\nReason: %s\n\nGather context that answers this gap specifically, then rebuild the pack.\n\nOriginal task:\n%s",
		reason, origin.Description)

	kinds := []TieredStepKind{TieredStepContext, TieredStepDecision, TieredStepExecute, TieredStepVerify}
	dag := make([]models.TieredDAGTask, 0, len(kinds))
	var previousID string
	for i, kind := range kinds {
		description := origin.Description
		if i == 0 {
			description = contextDescription
		}
		step := w.newTieredStep(*origin, kind, description, now)
		if i == 0 {
			step.State = models.TaskStateReady
		} else {
			step.DependsOn = []string{previousID}
		}
		dag = append(dag, models.TieredDAGTask{Task: step, DependsOnID: previousID})
		previousID = step.ID
	}

	if _, err := w.store.PersistTieredDAG(ctx, origin.ID, origin.UpdatedAt, dag); err != nil {
		return fmt.Errorf("persist re-gather chain: %w", err)
	}
	return nil
}

// latestResultPayload returns the most recent RESULT event payload for a task.
func (w *Worker) latestResultPayload(ctx context.Context, task models.Task) (string, error) {
	events, err := w.store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		return "", fmt.Errorf("read task events: %w", err)
	}
	payload := ""
	for _, ev := range events {
		if ev.Type == models.EventTypeResult {
			payload = ev.Payload
		}
	}
	if strings.TrimSpace(payload) == "" {
		return "", fmt.Errorf("no RESULT event payload on task %s", task.ID)
	}
	return payload, nil
}
