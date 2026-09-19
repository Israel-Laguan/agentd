package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"agentd/internal/models"
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

// DecisionArtifact is the structured output from a decision step.
type DecisionArtifact struct {
	TouchList  []string `json:"touch_list"`
	Checks     []string `json:"checks"`
	Rationale  string   `json:"rationale,omitempty"`
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
// Note: wired in T-020 (called from worker_tiered.go verify completion path).
//
//nolint:unused // Wired in T-020
func (w *Worker) handleVerifyOutcome(
	ctx context.Context,
	task models.Task,
	outcome models.VerifyResultOutcome,
	verifyResult VerifyResult,
	parentTask models.Task,
) error {
	switch outcome {
	case models.VerifyOutcomePass:
		// Step succeeded; transition parent from BLOCKED → READY then COMPLETED
		slog.InfoContext(ctx, "tiered verify passed", "task_id", task.ID, "parent_id", parentTask.ID)
		// First move from BLOCKED to READY (intermediate state)
		if err := w.transitionTaskState(ctx, parentTask.ID, models.TaskStateReady); err != nil {
			return err
		}
		// Then complete it
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

// scheduleMidFix creates a bounded redo of execute+verify with the same pack.
// Note: called from handleVerifyOutcome; full integration wired in T-020.
//
//nolint:unused // Called from handleVerifyOutcome
func (w *Worker) scheduleMidFix(ctx context.Context, verifyTask models.Task, parentTask models.Task, evidence VerifyResult) error {
	midFixCount := getMetadataInt(verifyTask, "mid_fix_passes", 0)
	maxMidFix := 2 // configurable per deployment

	if midFixCount >= maxMidFix {
		// Exhausted mid fix attempts; escalate with evidence preserved
		slog.InfoContext(ctx, "mid fix attempts exhausted; escalating", "task_id", verifyTask.ID, "passes", midFixCount)
		return w.scheduleEscalation(ctx, verifyTask, parentTask, evidence)
	}

	// Get the execute task to re-run (find predecessor via DEPENDS_ON)
	executeTask, err := w.getPredecessorTask(ctx, verifyTask)
	if err != nil || executeTask == nil {
		return fmt.Errorf("failed to find execute task for mid fix: %w", err)
	}

	// Create an execute redo task using the same agent profile as the original execute
	midFixTask := models.DraftTask{
		Title:       fmt.Sprintf("Mid-fix redo: %s", parentTask.Title),
		Description: fmt.Sprintf("Bounded redo of execute after verify failure (attempt %d)", midFixCount+1),
		AgentID:     "tier-execute", // Use registered profile, not synthetic ID
	}

	// Store metadata in task logs
	logs := map[string]interface{}{
		"mid_fix_passes":       midFixCount + 1,
		"parent_tiered_dag_id": parentTask.ID,
		"context_pack_version": getMetadata(parentTask, "context_pack_version", "1"),
		"step_kind":            "execute",
	}
	logsJSON, _ := json.Marshal(logs)
	midFixTask.SuccessCriteria = []string{string(logsJSON)}

	// Attach mid-fix to the execute task (not parent) to avoid dependency cycle
	if err := w.createAndSpawnTask(ctx, midFixTask, *executeTask); err != nil {
		return fmt.Errorf("failed to schedule mid fix: %w", err)
	}

	slog.InfoContext(ctx, "mid fix scheduled", "attempt", midFixCount+1)
	return nil
}

// scheduleEscalation creates an escalate step with strong model.
// Note: called from scheduleMidFix on exhaustion; full integration wired in T-020.
//
//nolint:unused // Called from scheduleMidFix
func (w *Worker) scheduleEscalation(ctx context.Context, verifyTask models.Task, parentTask models.Task, evidence VerifyResult) error {
	escalateCount := getMetadataInt(parentTask, "escalate_count", 0)
	maxEscalate := 1 // configurable per deployment

	if escalateCount >= maxEscalate {
		// Exhausted escalation; hand off to HUMAN
		slog.InfoContext(ctx, "escalation limit reached; handing off to human", "task_id", parentTask.ID)
		return w.transitionTaskState(ctx, parentTask.ID, models.TaskStateFailedRequiresHuman)
	}

	// Create escalate step using registered profile (strong model)
	escalateTask := models.DraftTask{
		Title:       fmt.Sprintf("Escalation: %s", parentTask.Title),
		Description: fmt.Sprintf("Strong model escalation after verify conflict"),
		AgentID:     "tier-escalate",
	}

	// Store evidence and metadata in task logs
	evidenceJSON, _ := json.Marshal(evidence)
	logs := map[string]interface{}{
		"verify_evidence":      string(evidenceJSON),
		"parent_tiered_dag_id": parentTask.ID,
		"context_pack_version": getMetadata(parentTask, "context_pack_version", "1"),
		"escalate_count":       escalateCount + 1,
		"step_kind":            "escalate",
	}
	logsJSON, _ := json.Marshal(logs)
	escalateTask.SuccessCriteria = []string{string(logsJSON)}

	// Get the verify task to attach escalation to (find predecessor)
	verifyPredecessor, err := w.getPredecessorTask(ctx, verifyTask)
	if err != nil || verifyPredecessor == nil {
		return fmt.Errorf("failed to find verify task predecessor for escalation: %w", err)
	}

	// Create and spawn the task attached to the verify predecessor, not the parent
	if err := w.createAndSpawnTask(ctx, escalateTask, *verifyPredecessor); err != nil {
		return fmt.Errorf("failed to schedule escalation: %w", err)
	}

	slog.InfoContext(ctx, "escalation scheduled")
	return nil
}

// getPredecessorTask finds the immediate predecessor task via DEPENDS_ON relation.
//
//nolint:unused // Called from scheduleMidFix and scheduleEscalation
func (w *Worker) getPredecessorTask(ctx context.Context, task models.Task) (*models.Task, error) {
	// Query DEPENDS_ON relations specifically for this task (tiered pipeline uses typed relations)
	predecessors, err := w.store.ListParentTasksByRelation(ctx, task.ID, models.TaskRelationDependsOn)
	if err != nil {
		return nil, fmt.Errorf("failed to list predecessor relations: %w", err)
	}

	if len(predecessors) == 0 {
		return nil, nil // No predecessor
	}

	if len(predecessors) > 1 {
		slog.WarnContext(ctx, "task has multiple DEPENDS_ON predecessors; using first", "task_id", task.ID)
	}

	return &predecessors[0], nil
}

// getDecisionArtifact retrieves the decision output from the decision step.
//
//nolint:unused // Called from execute/verify steps (T-020)
func (w *Worker) getDecisionArtifact(ctx context.Context, decisionTask models.Task) (*DecisionArtifact, error) {
	// Decision artifact is stored in task metadata or logs
	artifactJSON := getMetadata(decisionTask, "decision_artifact", "")
	if artifactJSON == "" {
		// Fallback: parse from logs if not in metadata
		// This is a simplified approach; in production, you'd extract from the actual task output
		return nil, fmt.Errorf("decision artifact not found for task %s", decisionTask.ID)
	}

	var artifact DecisionArtifact
	if err := json.Unmarshal([]byte(artifactJSON), &artifact); err != nil {
		return nil, fmt.Errorf("failed to parse decision artifact: %w", err)
	}

	return &artifact, nil
}

// createAndSpawnTask creates a new task using AppendTasksToProject.
//
//nolint:unused // Called from scheduleMidFix and scheduleEscalation
func (w *Worker) createAndSpawnTask(ctx context.Context, draftTask models.DraftTask, parentTask models.Task) error {
	// Use AppendTasksToProject which handles SPAWNED_BY relation automatically
	tasks, err := w.store.AppendTasksToProject(ctx, parentTask.ProjectID, parentTask.ID, []models.DraftTask{draftTask})
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no tasks created")
	}

	slog.InfoContext(ctx, "task spawned", "task_id", tasks[0].ID, "parent_id", parentTask.ID)
	return nil
}

// transitionTaskState moves a task to a new state.
//
//nolint:unused // Called from handleVerifyOutcome
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

// Helper functions for task metadata management

//nolint:unused // Used by escalation functions and scheduled via T-020
func getMetadata(task models.Task, key, defaultVal string) string {
	// Metadata is stored in task logs as JSON
	if task.Logs == "" {
		return defaultVal
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(task.Logs), &meta); err != nil {
		return defaultVal
	}
	if val, ok := meta[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
		if num, ok := val.(float64); ok {
			return fmt.Sprint(num)
		}
		return fmt.Sprint(val)
	}
	return defaultVal
}

func setMetadata(task *models.Task, key, value string) {
	// Store in Logs field as JSON or structured format
	if task.Logs == "" {
		task.Logs = "{}"
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(task.Logs), &meta); err != nil {
		meta = make(map[string]interface{})
	}
	meta[key] = value
	if data, err := json.Marshal(meta); err == nil {
		task.Logs = string(data)
	}
}

func getMetadataInt(task models.Task, key string, defaultVal int) int {
	val := getMetadata(task, key, "")
	if val == "" {
		return defaultVal
	}
	var i int
	if _, err := fmt.Sscanf(val, "%d", &i); err != nil {
		return defaultVal
	}
	return i
}

func setMetadataInt(task *models.Task, key string, value int) {
	// Format value as string and store in metadata
	valStr := fmt.Sprintf("%d", value)
	setMetadata(task, key, valStr)
}
