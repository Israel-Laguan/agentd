package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

// midFixTitlePrefix marks a mid-fix redo task's Title so countMidFixAttempts
// can tell it apart from the DAG's original execute step, which shares the
// same AgentID ("tier-execute") so it dispatches through the identical
// tiered execute path (tool allowlist, ContextPack injection, prompt).
const midFixTitlePrefix = "Mid-fix redo: "

// escalateAgentID is the AgentID assigned to a strong-model escalation
// task. It intentionally does not match any of the four base tiered step
// kinds (context/decision/execute/verify): escalation is a one-off,
// last-resort attempt, not a redo through the fixed DAG shape.
const escalateAgentID = "tier-escalate"

// ClassifyVerifyOutcome maps verify result to an outcome type.
func ClassifyVerifyOutcome(verifyResult VerifyResult) models.VerifyResultOutcome {
	if verifyResult.Overall == "pass" {
		return models.VerifyOutcomePass
	}
	// Check for conflicts in the results
	for _, result := range verifyResult.Results {
		if result.Outcome == "conflict" {
			return models.VerifyOutcomeConflict
		}
	}
	// Check for flakes
	for _, result := range verifyResult.Results {
		if result.Outcome == "flake" {
			return models.VerifyOutcomeFlake
		}
	}
	return models.VerifyOutcomeFail
}

// handleVerifyOutcome routes the verify result through the escalation ladder.
func (w *Worker) handleVerifyOutcome(
	ctx context.Context,
	task models.Task,
	outcome models.VerifyResultOutcome,
	verifyResult VerifyResult,
	parentTask models.Task,
) error {
	switch outcome {
	case models.VerifyOutcomePass:
		// Step succeeded; mark parent as complete
		slog.InfoContext(ctx, "tiered verify passed", "task_id", task.ID, "parent_id", parentTask.ID)
		return w.transitionTaskState(ctx, parentTask.ID, models.TaskStateCompleted)

	case models.VerifyOutcomeFlake:
		// Intermittent failure; retry with mid fix (bounded redo)
		slog.InfoContext(ctx, "tiered verify flake detected; triggering mid fix", "task_id", task.ID)
		return w.scheduleMidFix(ctx, task, parentTask, verifyResult)

	case models.VerifyOutcomeFail:
		// Hard failure; try mid fix (bounded redo)
		slog.InfoContext(ctx, "tiered verify fail detected; triggering mid fix", "task_id", task.ID)
		return w.scheduleMidFix(ctx, task, parentTask, verifyResult)

	case models.VerifyOutcomeConflict:
		// Design conflict; escalate to strong model
		slog.InfoContext(ctx, "tiered verify conflict detected; scheduling escalation", "task_id", task.ID)
		return w.scheduleEscalation(ctx, task, parentTask, verifyResult)

	default:
		return fmt.Errorf("unknown verify outcome: %v", outcome)
	}
}

// countMidFixAttempts returns how many mid-fix redo tasks have already been
// spawned for this pipeline's origin, by counting persisted SPAWNED_BY
// children rather than trusting an in-memory counter that never survives a
// task reload. This is what makes the mid-fix cap durable across separate
// worker dispatch cycles.
func (w *Worker) countMidFixAttempts(ctx context.Context, originID string) (int, error) {
	children, err := w.store.ListChildTasksByRelation(ctx, originID, models.TaskRelationSpawnedBy)
	if err != nil {
		return 0, fmt.Errorf("list spawned children: %w", err)
	}
	count := 0
	for _, child := range children {
		if strings.HasPrefix(child.Title, midFixTitlePrefix) {
			count++
		}
	}
	return count, nil
}

// countEscalations returns how many escalation tasks have already been
// spawned for this pipeline's origin. See countMidFixAttempts.
func (w *Worker) countEscalations(ctx context.Context, originID string) (int, error) {
	children, err := w.store.ListChildTasksByRelation(ctx, originID, models.TaskRelationSpawnedBy)
	if err != nil {
		return 0, fmt.Errorf("list spawned children: %w", err)
	}
	count := 0
	for _, child := range children {
		if child.AgentID == escalateAgentID {
			count++
		}
	}
	return count, nil
}

// scheduleMidFix creates a bounded redo of execute with the same pack.
func (w *Worker) scheduleMidFix(ctx context.Context, verifyTask models.Task, parentTask models.Task, verifyResult VerifyResult) error {
	midFixCount, err := w.countMidFixAttempts(ctx, parentTask.ID)
	if err != nil {
		return fmt.Errorf("count mid fix attempts: %w", err)
	}
	// Route the cap check through getMetadata/setMetadata (rather than
	// comparing midFixCount directly) so the durable, DAG-derived count and
	// the task.Logs metadata API agree on the same number.
	setMetadataInt(&verifyTask, "mid_fix_passes", midFixCount)
	maxMidFix := w.tieredCfg.EscalationConfig().MaxMidFix

	if getMetadataInt(verifyTask, "mid_fix_passes", 0) >= maxMidFix {
		// Exhausted mid fix attempts; escalate
		slog.InfoContext(ctx, "mid fix attempts exhausted; escalating", "task_id", verifyTask.ID, "passes", midFixCount)
		return w.scheduleEscalation(ctx, verifyTask, parentTask, verifyResult)
	}

	assignee := parentTask.Assignee
	if !assignee.Valid() {
		assignee = models.TaskAssigneeSystem
	}
	now := time.Now().UTC()
	midFixTask := models.Task{
		BaseEntity: models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:  parentTask.ProjectID,
		// AgentID matches the DAG's own execute step so this redo dispatches
		// through the identical tiered execute path (tool allowlist,
		// ContextPack injection, execute prompt) rather than a bespoke one.
		AgentID:     tieredStepProfile[TieredStepExecute],
		Title:       fmt.Sprintf("%s%s", midFixTitlePrefix, parentTask.Title),
		Description: fmt.Sprintf("Bounded redo of execute after verify failure (attempt %d).\n\n%s", midFixCount+1, parentTask.Description),
		State:       models.TaskStateReady,
		Assignee:    assignee,
	}

	if _, err := w.store.SpawnTieredContinuation(ctx, parentTask.ID, []models.TieredContinuationTask{{Task: midFixTask}}); err != nil {
		return fmt.Errorf("failed to schedule mid fix: %w", err)
	}

	slog.InfoContext(ctx, "mid fix scheduled", "attempt", midFixCount+1)
	return nil
}

// scheduleEscalation creates an escalate step with strong model.
func (w *Worker) scheduleEscalation(ctx context.Context, verifyTask models.Task, parentTask models.Task, evidence VerifyResult) error {
	escalateCount, err := w.countEscalations(ctx, parentTask.ID)
	if err != nil {
		return fmt.Errorf("count escalation attempts: %w", err)
	}
	setMetadataInt(&parentTask, "escalate_count", escalateCount)
	maxEscalate := w.tieredCfg.EscalationConfig().MaxEscalate

	if getMetadataInt(parentTask, "escalate_count", 0) >= maxEscalate {
		// Exhausted escalation; hand off to HUMAN
		slog.InfoContext(ctx, "escalation limit reached; handing off to human", "task_id", parentTask.ID)
		return w.transitionTaskState(ctx, parentTask.ID, models.TaskStateFailedRequiresHuman)
	}

	assignee := parentTask.Assignee
	if !assignee.Valid() {
		assignee = models.TaskAssigneeSystem
	}
	evidenceJSON, _ := json.Marshal(evidence)
	now := time.Now().UTC()
	escalateTask := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   parentTask.ProjectID,
		AgentID:     escalateAgentID,
		Title:       fmt.Sprintf("Escalation: %s", parentTask.Title),
		Description: fmt.Sprintf("Strong model escalation after verify conflict.\n\nVerify evidence:\n%s\n\nOriginal task:\n%s", string(evidenceJSON), parentTask.Description),
		State:       models.TaskStateReady,
		Assignee:    assignee,
	}

	if _, err := w.store.SpawnTieredContinuation(ctx, parentTask.ID, []models.TieredContinuationTask{{Task: escalateTask}}); err != nil {
		return fmt.Errorf("failed to schedule escalation: %w", err)
	}

	slog.InfoContext(ctx, "escalation scheduled")
	return nil
}

// transitionTaskState moves a task to a new state.
func (w *Worker) transitionTaskState(ctx context.Context, taskID string, newState models.TaskState) error {
	task, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	if !task.State.CanTransitionTo(newState) {
		return fmt.Errorf("invalid transition from %s to %s", task.State, newState)
	}

	// Use UpdateTaskState with proper concurrency control
	_, err = w.store.UpdateTaskState(ctx, taskID, task.UpdatedAt, newState)
	return err
}

// Helper functions for task metadata management live in tiered_metadata.go.

