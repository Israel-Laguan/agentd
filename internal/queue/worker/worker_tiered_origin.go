package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"agentd/internal/models"
)

// tryResolveTieredOrigin finalizes the origin task of a tiered pipeline.
//
// The origin is BLOCKED while its steps run. When the last step resolves, the
// store unblocks it back to READY and the queue re-dispatches it — so the
// origin arrives here RUNNING, having already had its work done by the DAG.
// It must not be re-split (that would loop forever) and it cannot be completed
// with UpdateTaskState either, since BLOCKED/READY -> COMPLETED is not a legal
// transition. Resolving it through UpdateTaskResult is the supported path.
//
// It reports whether the task was handled; false means this is not a tiered
// origin and the caller should continue with the normal dispatch path.
func (w *Worker) tryResolveTieredOrigin(ctx context.Context, task models.Task) bool {
	steps, err := w.tieredStepChildren(ctx, task)
	if err != nil {
		slog.Error("tiered: failed to list origin steps", "task_id", task.ID, "error", err)
		return false
	}
	if len(steps) == 0 {
		return false
	}

	if pending := unresolvedSteps(steps); pending > 0 {
		// Steps are still outstanding (the ladder just appended a redo, or the
		// origin was unblocked early). Go back to BLOCKED and wait for them.
		slog.Info("tiered origin: steps still outstanding; re-blocking",
			"task_id", task.ID, "pending", pending)
		w.reblockTieredOrigin(ctx, task)
		return true
	}

	outcome, found, err := w.latestVerifyOutcome(ctx, steps)
	if err != nil {
		slog.Error("tiered origin: failed to determine verify outcome", "task_id", task.ID, "error", err)
		w.failTieredOrigin(ctx, task, "tiered pipeline: failed to read verify events")
		return true
	}
	if !found {
		// No verify step ever recorded a verdict — e.g. every verify attempt
		// handed off, suspended, fell back to legacy execution, or exhausted
		// its budget/turn-limit/tool-retries before classifying an outcome.
		// All steps being COMPLETED does NOT mean the work was verified, so
		// this must resolve as failure, never success: reporting success here
		// would let unverified changes look done.
		w.finishTieredOrigin(ctx, task, false, "tiered pipeline finished without a verify verdict")
		return true
	}
	if outcome == models.VerifyOutcomePass {
		w.finishTieredOrigin(ctx, task, true, "tiered pipeline verified")
		return true
	}

	// The last verify did not pass, so the ladder owns the next move: it either
	// appended a redo (caught by the pending check above on the next pass) or
	// handed off to a human. Stay BLOCKED rather than racing it to a verdict.
	slog.Info("tiered origin: awaiting escalation ladder", "task_id", task.ID, "outcome", outcome)
	w.reblockTieredOrigin(ctx, task)
	return true
}

// tieredStepChildren returns the tiered step tasks spawned by the origin.
func (w *Worker) tieredStepChildren(ctx context.Context, origin models.Task) ([]models.Task, error) {
	children, err := w.store.ListChildTasksByRelation(ctx, origin.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		return nil, err
	}
	steps := make([]models.Task, 0, len(children))
	for _, child := range children {
		if _, ok := tieredStepProfiles[child.AgentID]; ok {
			steps = append(steps, child)
		}
	}
	return steps, nil
}

// unresolvedSteps counts steps that have not reached a terminal state.
// NEEDS_CONTEXT counts as resolved: such a step was abandoned by a re-gather
// and superseded by a newer chain, so the origin must not wait on it.
func unresolvedSteps(steps []models.Task) int {
	pending := 0
	for _, step := range steps {
		switch step.State {
		case models.TaskStateCompleted, models.TaskStateFailed,
			models.TaskStateFailedRequiresHuman, models.TaskStateNeedsContext:
		default:
			pending++
		}
	}
	return pending
}

// latestVerifyOutcome returns the verdict of the most recently created verify
// step that actually recorded one. Verify steps abandoned by a re-gather, or
// that never ran, carry no verdict and are skipped rather than treated as a
// missing result for the whole pipeline.
func (w *Worker) latestVerifyOutcome(ctx context.Context, steps []models.Task) (models.VerifyResultOutcome, bool, error) {
	verifies := make([]models.Task, 0, len(steps))
	for _, step := range steps {
		if tieredStepProfiles[step.AgentID] == TieredStepVerify {
			verifies = append(verifies, step)
		}
	}
	sort.Slice(verifies, func(i, j int) bool {
		return verifies[i].CreatedAt.After(verifies[j].CreatedAt)
	})
	for _, verify := range verifies {
		outcome, ok, err := w.recordedVerifyOutcome(ctx, verify)
		if err != nil {
			slog.Error("tiered origin: failed to read verify events", "task_id", verify.ID, "error", err)
			return "", false, err
		}
		if ok {
			return outcome, true, nil
		}
	}
	return "", false, nil
}

// recordedVerifyOutcome reads the verdict a verify step emitted, if any.
func (w *Worker) recordedVerifyOutcome(ctx context.Context, verify models.Task) (models.VerifyResultOutcome, bool, error) {
	events, err := w.store.ListEventsByTask(ctx, verify.ID)
	if err != nil {
		return "", false, fmt.Errorf("failed to read verify events for task %s: %w", verify.ID, err)
	}
	outcome := models.VerifyResultOutcome("")
	for _, ev := range events {
		if string(ev.Type) == tieredVerifyOutcomeEvent {
			outcome = models.VerifyResultOutcome(ev.Payload)
		}
	}
	if !outcome.Valid() {
		return "", false, nil
	}
	return outcome, true, nil
}

// finishTieredOrigin records the pipeline's verdict on the origin task.
func (w *Worker) finishTieredOrigin(ctx context.Context, task models.Task, success bool, reason string) {
	if _, err := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{
		Success: success,
		Payload: truncate(reason, 1000),
	}); err != nil {
		slog.Error("tiered origin: failed to record pipeline result",
			"task_id", task.ID, "success", success, "error", err)
		w.Emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.Emit(ctx, task, "TIERED_PIPELINE_RESOLVED", fmt.Sprintf("success=%t %s", success, reason))
}

// reblockTieredOrigin returns the origin to BLOCKED while steps are pending.
func (w *Worker) reblockTieredOrigin(ctx context.Context, task models.Task) {
	if _, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
		if errors.Is(err, models.ErrStateConflict) {
			return
		}
		slog.Error("tiered origin: failed to re-block", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "ERROR", err.Error())
	}
}

// completeTieredOrigin resolves the origin as succeeded from a non-RUNNING
// state. UpdateTaskResult only accepts RUNNING tasks, so a BLOCKED or READY
// origin first walks the legal BLOCKED→READY→RUNNING ladder (preserving
// optimistic-concurrency checks via UpdateTaskState) before recording the
// result.
func (w *Worker) completeTieredOrigin(ctx context.Context, origin models.Task, reason string) {
	current, err := w.store.GetTask(ctx, origin.ID)
	if err != nil {
		slog.Error("tiered origin: failed to re-read before completing", "task_id", origin.ID, "error", err)
		return
	}
	for current.State == models.TaskStateBlocked || current.State == models.TaskStateReady {
		next := models.TaskStateRunning
		if current.State == models.TaskStateBlocked {
			next = models.TaskStateReady
		}
		updated, err := w.store.UpdateTaskState(ctx, current.ID, current.UpdatedAt, next)
		if err != nil {
			slog.Error("tiered origin: failed to ready before completing",
				"task_id", origin.ID, "state", current.State, "error", err)
			w.Emit(ctx, *current, "ERROR", err.Error())
			return
		}
		current = updated
	}
	if current.State != models.TaskStateRunning {
		slog.Error("tiered origin: cannot complete from current state",
			"task_id", origin.ID, "state", current.State)
		return
	}
	w.finishTieredOrigin(ctx, *current, true, reason)
}

// failTieredOrigin resolves the origin as failed from a non-RUNNING state.
// UpdateTaskResult only accepts RUNNING tasks, so a BLOCKED origin whose
// ladder broke down has to be failed through a state transition instead.
func (w *Worker) failTieredOrigin(ctx context.Context, origin models.Task, reason string) {
	current, err := w.store.GetTask(ctx, origin.ID)
	if err != nil {
		slog.Error("tiered origin: failed to re-read before failing", "task_id", origin.ID, "error", err)
		return
	}
	if current.State == models.TaskStateRunning {
		w.finishTieredOrigin(ctx, *current, false, reason)
		return
	}
	if !current.State.CanTransitionTo(models.TaskStateFailed) {
		slog.Error("tiered origin: cannot fail from current state",
			"task_id", origin.ID, "state", current.State)
		return
	}
	if _, err := w.store.UpdateTaskState(ctx, current.ID, current.UpdatedAt, models.TaskStateFailed); err != nil {
		slog.Error("tiered origin: failed to mark failed", "task_id", origin.ID, "error", err)
		return
	}
	w.Emit(ctx, *current, "TIERED_PIPELINE_RESOLVED", "success=false "+reason)
}
