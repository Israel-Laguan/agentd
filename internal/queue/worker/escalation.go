package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

// Caps on the escalation ladder. Both are counted from the steps already on
// the board, so a restart mid-ladder resumes with the same budget.
const (
	maxMidFixPasses = 2
	maxEscalations  = 1
)

// VerifyResult captures the output of a verify step.
type VerifyResult struct {
	Results []CheckResult `json:"results"`
	Overall string        `json:"overall"`
}

// CheckResult records a single check execution.
type CheckResult struct {
	Check   string `json:"check"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
}

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
	originTask models.Task,
) error {
	switch outcome {
	case models.VerifyOutcomePass:
		// Nothing to schedule. The verify step's own successful result
		// unblocks the origin, which then resolves itself from the recorded
		// verdict in tryResolveTieredOrigin.
		slog.InfoContext(ctx, "tiered verify passed", "task_id", task.ID, "origin_id", originTask.ID)
		return nil

	case models.VerifyOutcomeFlake, models.VerifyOutcomeFail:
		slog.InfoContext(ctx, "tiered verify failed; triggering mid fix",
			"task_id", task.ID, "outcome", outcome)
		return w.scheduleMidFix(ctx, originTask, verifyResult)

	case models.VerifyOutcomeConflict:
		// Design conflict; skip the mid-fix rung and escalate.
		slog.InfoContext(ctx, "tiered verify conflict detected; scheduling escalation", "task_id", task.ID)
		return w.scheduleEscalation(ctx, originTask, verifyResult)

	default:
		return fmt.Errorf("unknown verify outcome: %v", outcome)
	}
}

// scheduleMidFix appends a bounded execute+verify redo against the same pack.
// Passes are counted from the verify steps already on the board rather than
// from a per-task counter, so the cap survives restarts and cannot drift.
func (w *Worker) scheduleMidFix(ctx context.Context, originTask models.Task, evidence VerifyResult) error {
	steps, err := w.tieredStepChildren(ctx, originTask)
	if err != nil {
		return fmt.Errorf("list tiered steps: %w", err)
	}
	// The initial DAG already contains one verify, so the number of redos so
	// far is one less than the number of verify steps.
	passes := countTieredSteps(steps, TieredStepVerify) - 1
	if passes >= maxMidFixPasses {
		slog.InfoContext(ctx, "mid fix attempts exhausted; escalating",
			"origin_id", originTask.ID, "passes", passes)
		return w.scheduleEscalation(ctx, originTask, evidence)
	}

	description := fmt.Sprintf(
		"Mid-fix redo (attempt %d of %d). The previous execute+verify cycle failed.\n\n%s\n\nOriginal task:\n%s",
		passes+1, maxMidFixPasses, formatVerifyEvidence(evidence), originTask.Description)
	if err := w.appendTieredRedo(ctx, originTask, TieredStepExecute, description); err != nil {
		return fmt.Errorf("schedule mid fix: %w", err)
	}
	slog.InfoContext(ctx, "mid fix scheduled", "origin_id", originTask.ID, "attempt", passes+1)
	return nil
}

// scheduleEscalation appends a strong-model escalate+verify pair, or hands the
// origin to a human once the escalation budget is spent.
func (w *Worker) scheduleEscalation(ctx context.Context, originTask models.Task, evidence VerifyResult) error {
	steps, err := w.tieredStepChildren(ctx, originTask)
	if err != nil {
		return fmt.Errorf("list tiered steps: %w", err)
	}
	if countTieredSteps(steps, TieredStepEscalate) >= maxEscalations {
		slog.InfoContext(ctx, "escalation limit reached; handing off to human", "origin_id", originTask.ID)
		return w.handoffTieredOriginToHuman(ctx, originTask)
	}

	description := fmt.Sprintf(
		"Escalation pass. Execute and verify could not resolve this task.\n\n%s\n\nOriginal task:\n%s",
		formatVerifyEvidence(evidence), originTask.Description)
	if err := w.appendTieredRedo(ctx, originTask, TieredStepEscalate, description); err != nil {
		return fmt.Errorf("schedule escalation: %w", err)
	}
	slog.InfoContext(ctx, "escalation scheduled", "origin_id", originTask.ID)
	return nil
}

// appendTieredRedo attaches a two-step redo chain (the given step kind, then a
// fresh verify) to the origin and blocks it again. Reusing PersistTieredDAG
// keeps the SPAWNED_BY / DEPENDS_ON shape identical to the initial split, so
// the redo dispatches through the same tiered path and re-closes the loop.
func (w *Worker) appendTieredRedo(ctx context.Context, originTask models.Task, kind TieredStepKind, description string) error {
	origin, err := w.originForAppend(ctx, originTask.ID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	redo := w.newTieredStep(*origin, kind, description, now)
	verify := w.newTieredStep(*origin, TieredStepVerify, originTask.Description, now)
	redo.State = models.TaskStateReady
	verify.State = models.TaskStatePending
	verify.DependsOn = []string{redo.ID}

	_, err = w.store.PersistTieredDAG(ctx, origin.ID, origin.UpdatedAt, []models.TieredDAGTask{
		{Task: redo},
		{Task: verify, DependsOnID: redo.ID},
	})
	if err != nil {
		return fmt.Errorf("persist redo chain: %w", err)
	}
	w.Emit(ctx, *origin, "TIERED_LADDER_STEP_ADDED", string(kind))
	return nil
}

// originForAppend re-reads the origin and puts it in a state PersistTieredDAG
// accepts. A fresh read matters because appending re-blocks the origin under
// optimistic concurrency and the step that got us here just bumped the row.
//
// PersistTieredDAG only accepts a RUNNING or READY parent. After a verify the
// origin has already been unblocked by the step's own result, but a re-gather
// raised mid-pipeline finds it still BLOCKED, so unblock it first —
// BLOCKED -> READY is the transition the store itself uses for this.
func (w *Worker) originForAppend(ctx context.Context, originID string) (*models.Task, error) {
	origin, err := w.store.GetTask(ctx, originID)
	if err != nil {
		return nil, fmt.Errorf("re-read origin: %w", err)
	}
	if origin.State != models.TaskStateBlocked {
		return origin, nil
	}
	unblocked, err := w.store.UpdateTaskState(ctx, origin.ID, origin.UpdatedAt, models.TaskStateReady)
	if err != nil {
		return nil, fmt.Errorf("unblock origin before append: %w", err)
	}
	return unblocked, nil
}

// newTieredStep builds a child task for the origin's pipeline.
func (w *Worker) newTieredStep(origin models.Task, kind TieredStepKind, description string, now time.Time) models.Task {
	assignee := origin.Assignee
	if !assignee.Valid() {
		assignee = models.TaskAssigneeSystem
	}
	return models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   origin.ProjectID,
		AgentID:     tieredStepProfile[kind],
		Title:       fmt.Sprintf("%s: %s", kind, origin.Title),
		Description: description,
		State:       models.TaskStatePending,
		Assignee:    assignee,
	}
}

// handoffTieredOriginToHuman moves the origin to FAILED_REQUIRES_HUMAN. The
// origin is BLOCKED at this point, which UpdateTaskResult would reject, so the
// handoff goes through a state transition instead.
func (w *Worker) handoffTieredOriginToHuman(ctx context.Context, originTask models.Task) error {
	origin, err := w.store.GetTask(ctx, originTask.ID)
	if err != nil {
		return fmt.Errorf("re-read origin: %w", err)
	}
	if origin.State == models.TaskStateFailedRequiresHuman {
		return nil
	}
	if !origin.State.CanTransitionTo(models.TaskStateFailedRequiresHuman) {
		return fmt.Errorf("cannot hand off origin from %s", origin.State)
	}
	if _, err := w.store.UpdateTaskState(ctx, origin.ID, origin.UpdatedAt, models.TaskStateFailedRequiresHuman); err != nil {
		return fmt.Errorf("hand off origin to human: %w", err)
	}
	w.Emit(ctx, *origin, "TIERED_ESCALATION_EXHAUSTED",
		"verify still failing after mid-fix and escalation budgets were spent")
	return nil
}

// countTieredSteps counts steps of the given kind.
func countTieredSteps(steps []models.Task, kind TieredStepKind) int {
	count := 0
	for _, step := range steps {
		if tieredStepProfiles[step.AgentID] == kind {
			count++
		}
	}
	return count
}

// formatVerifyEvidence renders the failing checks for the next step's prompt.
func formatVerifyEvidence(evidence VerifyResult) string {
	if len(evidence.Results) == 0 {
		return "FAILING VERIFY EVIDENCE: none recorded."
	}
	var b strings.Builder
	b.WriteString("FAILING VERIFY EVIDENCE:\n")
	for _, result := range evidence.Results {
		if result.Outcome == "pass" {
			continue
		}
		fmt.Fprintf(&b, "- [%s] %s: %s\n", result.Outcome, result.Check, result.Detail)
	}
	return strings.TrimRight(b.String(), "\n")
}
