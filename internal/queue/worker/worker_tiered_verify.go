package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/models"
)

const tieredVerifyOutcomeEvent = "TIERED_VERIFY_OUTCOME"

// processTieredVerifyStep runs the verify step through the agentic engine,
// then — unlike the generic tiered dispatch — reads back the committed
// VerifyResult JSON, classifies it, and routes it through the escalation
// ladder (handleVerifyOutcome). The verify task itself always ends up
// COMPLETED once the model produces an answer (the engine commits that
// unconditionally); what varies is what happens to the pipeline's origin
// task as a result.
func (w *Worker) processTieredVerifyStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, parentTask models.Task) {
	result, ok := w.processAgentic(ctx, task, project, profile)
	if !ok {
		w.failTieredStep(ctx, task, "tiered verify step produced no VerifyResult")
		w.failTieredDependents(ctx, task, parentTask)
		return
	}
	if !result.IsTerminalSuccess() {
		w.handleLoopResult(ctx, task, result)
		return
	}

	verifyResult, err := w.readVerifyResult(ctx, task)
	if err != nil {
		slog.Error("tiered verify: failed to parse VerifyResult", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_VERIFY_PARSE_ERROR", err.Error())
		return
	}

	outcome := ClassifyVerifyOutcome(*verifyResult)
	w.Emit(ctx, task, "TIERED_VERIFY_CLASSIFIED", string(outcome))
	if err := w.handleVerifyOutcome(ctx, task, outcome, *verifyResult, parentTask); err != nil {
		slog.Error("tiered verify: failed to route outcome", "task_id", task.ID, "outcome", outcome, "error", err)
		w.Emit(ctx, task, "TIERED_VERIFY_OUTCOME_ERROR", err.Error())
	}
}

// readVerifyResult reads the verify task's just-committed RESULT event and
// parses its payload as a VerifyResult.
func (w *Worker) readVerifyResult(ctx context.Context, task models.Task) (*VerifyResult, error) {
	payload, err := w.readCommittedText(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	return parseVerifyResult(payload)
}

// parseVerifyResult extracts a VerifyResult JSON object from raw model
// output, tolerating markdown code fences the way parseContextPack does.
func parseVerifyResult(output string) (*VerifyResult, error) {
	output = strings.TrimSpace(output)
	var vr VerifyResult
	if err := json.Unmarshal([]byte(output), &vr); err == nil {
		return &vr, nil
	}
	for _, fence := range []string{"```json", "```"} {
		if idx := strings.Index(output, fence); idx != -1 {
			start := idx + len(fence)
			if end := strings.Index(output[start:], "```"); end != -1 {
				jsonStr := strings.TrimSpace(output[start : start+end])
				if err := json.Unmarshal([]byte(jsonStr), &vr); err == nil {
					return &vr, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no valid VerifyResult JSON found in output")
}

// processTieredEscalateStep runs the strong-model escalation attempt and
// reconciles the pipeline's origin task directly: unlike the base DAG
// steps, escalation is a last resort, so its failure path skips the normal
// retry/healing loop and hands the whole pipeline to a human immediately.
func (w *Worker) processTieredEscalateStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, parentTask models.Task) {
	result, ok := w.processAgentic(ctx, task, project, profile)
	if !ok {
		w.failTieredStep(ctx, task, "tiered escalate step produced no result")
		if err := w.transitionTaskState(ctx, parentTask.ID, models.TaskStateFailedRequiresHuman); err != nil {
			slog.Error("tiered escalate: failed to hand off origin after no-result", "task_id", parentTask.ID, "error", err)
		}
		return
	}
	if !result.IsTerminalSuccess() {
		w.Emit(ctx, task, "TIERED_ESCALATE_FAILED", result.Status.String())
		if _, err := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{
			Success: false,
			Payload: "tiered escalation attempt failed: " + result.Status.String(),
		}); err != nil {
			slog.Error("tiered escalate: failed to record failure", "task_id", task.ID, "error", err)
		}
		if err := w.transitionTaskState(ctx, parentTask.ID, models.TaskStateFailedRequiresHuman); err != nil {
			slog.Error("tiered escalate: failed to hand off origin after failure", "task_id", parentTask.ID, "error", err)
		}
		return
	}

	// Escalation succeeded; trust the strong model's fix and complete the pipeline.
	if err := w.transitionTaskState(ctx, parentTask.ID, models.TaskStateCompleted); err != nil {
		slog.Error("tiered escalate: failed to complete origin after success", "task_id", parentTask.ID, "error", err)
	}
}
