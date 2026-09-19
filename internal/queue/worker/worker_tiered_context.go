package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"agentd/internal/models"
)

// processTieredContextStep runs the context step: uses the agentic engine
// to gather workspace context, then intercepts the committed text to
// parse, validate, and persist the ContextPack.
func (w *Worker) processTieredContextStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, parentTask models.Task) {
	// A tiered context step must run through the agentic engine: legacy
	// one-shot mode produces a shell command, not a ContextPack. If the
	// selected provider cannot round-trip tools, the engine would fall back
	// to legacy execution and commit a *successful* result with no pack,
	// letting decision/execute/verify run without the required artifact.
	// Fail the step up front instead.
	if !w.providerSupportsAgentic(profile) {
		slog.Warn("tiered context: provider does not support chat tools; failing step",
			"task_id", task.ID, "provider", profile.Provider)
		w.Emit(ctx, task, "TIERED_CONTEXT_PROVIDER_UNSUPPORTED", profile.Provider)
		w.failTieredStep(ctx, task, "tiered context step requires a provider with chat-tool support; got provider "+profile.Provider)
		return
	}
	result, ok := w.processAgentic(ctx, task, project, profile)
	if !ok {
		// The engine exits without a LoopResult when it handed off, suspended,
		// or fell back to legacy execution. No ContextPack can be parsed here,
		// so record the failure (or surface the conflict if the step was
		// already resolved) rather than leaving the DAG looking satisfied.
		w.failTieredStep(ctx, task, "tiered context step produced no ContextPack result")
		return
	}
	if !result.IsTerminalSuccess() {
		w.handleLoopResult(ctx, task, result)
		return
	}
	pack, committed, err := w.parseAndConfigurePack(ctx, task, parentTask)
	if err != nil {
		return
	}
	if err := w.validateAndWritePack(ctx, *committed, project, pack); err != nil {
		return
	}
	w.Emit(ctx, task, "TIERED_CONTEXT_PACK_WRITTEN", PackFilePath(pack.ParentTaskID, pack.Version))
}

// failTieredStep records a failed result against the task's latest known
// version so a malformed or unpersisted tiered artifact doesn't leave the
// step (and the DAG built on top of it) looking successful.
func (w *Worker) failTieredStep(ctx context.Context, task models.Task, reason string) {
	result := models.TaskResult{Success: false, Payload: truncate(reason, 1000)}
	if _, err := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, result); err != nil {
		if errors.Is(err, models.ErrStateConflict) {
			// The step left RUNNING at the expected version before we could
			// record the failure (e.g. a concurrent commit or heartbeat bump
			// moved it out from under us). Re-read the current state to
			// distinguish a legitimate COMPLETED-without-artifact scenario
			// from an engine-initiated failure that already recorded the
			// result — the latter does not warrant an escalation.
			current, getErr := w.store.GetTask(ctx, task.ID)
			if getErr != nil {
				slog.Error("tiered: failed to re-read task after state conflict",
					"task_id", task.ID, "error", getErr)
				w.Emit(ctx, task, "ERROR", getErr.Error())
				return
			}
			if current.State == models.TaskStateFailed {
				slog.Debug("tiered: task already FAILED by engine; skipping redundant failure",
					"task_id", task.ID, "reason", reason)
				return
			}
			if current.State == models.TaskStateRunning {
				// Still legitimately RUNNING (e.g. a heartbeat bumped
				// UpdatedAt) — retry once against the fresh version instead
				// of leaving the step stuck looking active.
				if _, retryErr := w.store.UpdateTaskResult(ctx, current.ID, current.UpdatedAt, result); retryErr != nil {
					slog.Error("tiered: retry failed to record step failure after state conflict",
						"task_id", task.ID, "reason", reason, "error", retryErr, "current_state", current.State)
					w.Emit(ctx, task, "TIERED_STEP_FAIL_CONFLICT",
						"escalation required: could not record failure ("+reason+"): "+retryErr.Error())
					return
				}
				return
			}
			slog.Error("tiered: failed to record step failure; task no longer RUNNING at expected version",
				"task_id", task.ID, "reason", reason, "error", err, "current_state", current.State)
			w.Emit(ctx, task, "TIERED_STEP_FAIL_CONFLICT",
				"escalation required: could not record failure ("+reason+"): "+err.Error())
			return
		}
		w.Emit(ctx, task, "ERROR", err.Error())
	}
}

// failTieredDependents finds all tasks that DEPENDS_ON the given task
// and marks them as failed so the pipeline does not remain stuck waiting
// for steps that can never execute because their predecessor failed.
// It also resolves the blocked origin task so the pipeline is not left
// in an indeterminate state.
func (w *Worker) failTieredDependents(ctx context.Context, failedTask models.Task, originTask models.Task) {
	dependents, err := w.store.ListChildTasksByRelation(ctx, failedTask.ID, models.TaskRelationDependsOn)
	if err != nil {
		slog.Error("tiered: failed to look up dependent tasks",
			"task_id", failedTask.ID, "error", err)
		return
	}
	for _, dep := range dependents {
		slog.Warn("tiered: failing dependent step due to predecessor failure",
			"task_id", dep.ID, "step", dep.AgentID, "predecessor", failedTask.ID)
		w.failTieredStep(ctx, dep, "Predecessor step failed; dependent step cancelled")
	}
	if _, err := w.store.UpdateTaskResult(ctx, originTask.ID, originTask.UpdatedAt, models.TaskResult{
		Success: false,
		Payload: truncate("Tiered step failed; pipeline cancelled", 1000),
	}); err != nil {
		if !errors.Is(err, models.ErrStateConflict) {
			slog.Error("tiered: failed to resolve origin task after tiered step failure",
				"task_id", originTask.ID, "error", err)
		}
	}
}

// parseAndConfigurePack reads the committed task result, parses the ContextPack,
// sets task IDs, and enforces budget limits from tiered config. It returns the
// freshly-read committed task so callers can use its UpdatedAt for further
// optimistic-locked writes (e.g. failing the step or persisting the pack).
func (w *Worker) parseAndConfigurePack(ctx context.Context, task models.Task, parentTask models.Task) (*ContextPack, *models.Task, error) {
	committed, err := w.store.GetTask(ctx, task.ID)
	if err != nil {
		slog.Error("tiered context: failed to read committed task", "task_id", task.ID, "error", err)
		return nil, nil, err
	}
	payload := committed.Description
	if payload == "" {
		slog.Warn("tiered context: committed result is empty", "task_id", task.ID)
		w.failTieredStep(ctx, *committed, "empty ContextPack result")
		return nil, committed, fmt.Errorf("empty committed result")
	}
	pack, err := parseContextPack(payload)
	if err != nil {
		slog.Error("tiered context: failed to parse ContextPack", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_CONTEXT_PARSE_ERROR", err.Error())
		w.failTieredStep(ctx, *committed, "invalid ContextPack: "+err.Error())
		return nil, committed, err
	}
	pack.TaskID = task.ID
	pack.ParentTaskID = parentTask.ID
	pack.Budget.PathCount = len(pack.Paths)
	pack.Budget.CharCount = pack.CharCount()
	packCfg := w.contextPackConfig()
	if _, err := pack.EnforceBudget(packCfg); err != nil {
		slog.Error("tiered context: budget enforcement failed", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_CONTEXT_BUDGET_ERROR", err.Error())
		w.failTieredStep(ctx, *committed, "ContextPack budget exceeded: "+err.Error())
		return nil, committed, err
	}
	return pack, committed, nil
}

// contextPackConfig returns the ContextPackConfig with tiered overrides applied.
func (w *Worker) contextPackConfig() ContextPackConfig {
	packCfg := DefaultContextPackConfig()
	if w.tieredCfg.ContextPack.MaxPaths > 0 {
		packCfg.MaxPaths = w.tieredCfg.ContextPack.MaxPaths
	}
	if w.tieredCfg.ContextPack.MaxChars > 0 {
		packCfg.MaxChars = w.tieredCfg.ContextPack.MaxChars
	}
	return packCfg
}

// validateAndWritePack validates the ContextPack and writes it to the workspace.
func (w *Worker) validateAndWritePack(ctx context.Context, task models.Task, project models.Project, pack *ContextPack) error {
	if err := pack.Validate(); err != nil {
		slog.Error("tiered context: pack validation failed", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_CONTEXT_VALIDATION_ERROR", err.Error())
		w.failTieredStep(ctx, task, "ContextPack validation failed: "+err.Error())
		return err
	}
	if err := WriteContextPack(project.WorkspacePath, pack); err != nil {
		slog.Error("tiered context: failed to write ContextPack", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_CONTEXT_WRITE_ERROR", err.Error())
		w.failTieredStep(ctx, task, "ContextPack write failed: "+err.Error())
		return err
	}
	return nil
}

// injectContextPack reads the ContextPack for this pipeline (scoped to the
// origin task) and prepends its summary to the task description so the
// downstream step has context. It returns the (possibly modified) task;
// callers must use the returned value, since task is passed by value here.
// Returns an error when the pack cannot be read or its lineage mismatches,
// so the caller can fail the tiered step instead of proceeding without context.
func (w *Worker) injectContextPack(task models.Task, parentTask models.Task, project models.Project) (models.Task, error) {
	scopedPath := filepath.Join(project.WorkspacePath, PackFilePath(parentTask.ID, ContextPackVersion))
	legacyPath := filepath.Join(project.WorkspacePath, PackFilePath("", ContextPackVersion))
	pack, err := ReadContextPackWithFallback(scopedPath, legacyPath)
	if err != nil {
		slog.Warn("tiered: failed to read ContextPack for injection",
			"task_id", task.ID, "scoped_path", scopedPath, "error", err)
		return task, fmt.Errorf("read ContextPack: %w", err)
	}
	// Defense in depth: never inject a pack produced by a different pipeline
	// if the scoped file still carries another task's lineage.
	if pack.ParentTaskID != "" && pack.ParentTaskID != parentTask.ID {
		slog.Warn("tiered: ContextPack lineage mismatch, skipping injection",
			"task_id", task.ID, "expected_parent", parentTask.ID, "pack_parent", pack.ParentTaskID)
		return task, fmt.Errorf("ContextPack lineage mismatch: expected parent %s, got %s", parentTask.ID, pack.ParentTaskID)
	}
	summary := fmt.Sprintf(
		"CONTEXT PACK (from %s step):\nSummary: %s\nPaths: %s\nConstraints: %s\nUnknowns: %s\n",
		TieredStepContext, pack.Summary, strings.Join(pack.Paths, ", "),
		strings.Join(pack.Constraints, "; "), strings.Join(pack.Unknowns, "; "))
	task.Description = summary + "\n\nOriginal task:\n" + task.Description
	return task, nil
}

// parseContextPack attempts to extract a ContextPack from the LLM output.
// The output may contain JSON embedded in markdown code fences or plain text.
func parseContextPack(output string) (*ContextPack, error) {
	output = strings.TrimSpace(output)
	var cp ContextPack
	if err := json.Unmarshal([]byte(output), &cp); err == nil {
		return &cp, nil
	}
	// Try extracting from markdown code fences
	if idx := strings.Index(output, "```json"); idx != -1 {
		start := idx + len("```json")
		if end := strings.Index(output[start:], "```"); end != -1 {
			jsonStr := strings.TrimSpace(output[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &cp); err == nil {
				return &cp, nil
			}
		}
	}
	if idx := strings.Index(output, "```"); idx != -1 {
		start := idx + len("```")
		if end := strings.Index(output[start:], "```"); end != -1 {
			jsonStr := strings.TrimSpace(output[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &cp); err == nil {
				return &cp, nil
			}
		}
	}
	return nil, fmt.Errorf("no valid ContextPack JSON found in output")
}
