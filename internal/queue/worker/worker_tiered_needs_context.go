package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/models"
)

type needsContextSignal struct {
	NeedsContext bool   `json:"needs_context"`
	Reason       string `json:"reason"`
}

// readCommittedText reads the most recent RESULT event, strips the exit/duration prefix.
func (w *Worker) readCommittedText(ctx context.Context, taskID string) (string, error) {
	events, err := w.store.ListEventsByTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("list events: %w", err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == models.EventTypeResult {
			payload := events[i].Payload
			if idx := strings.IndexByte(payload, '\n'); idx != -1 {
				payload = payload[idx+1:]
			}
			return payload, nil
		}
	}
	return "", fmt.Errorf("no RESULT event found for task %s", taskID)
}

// processTieredDecisionStep runs the decision step, checks its
// committed output for NEEDS_CONTEXT, then reconciles.
func (w *Worker) processTieredDecisionStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, parentTask models.Task) {
	if !w.providerSupportsAgentic(profile) {
		w.failTieredStep(ctx, task, "tiered decision step requires an agentic-capable provider")
		w.failTieredDependents(ctx, task, parentTask)
		return
	}
	result, ok := w.processAgentic(ctx, task, project, profile)
	if !ok {
		w.failTieredStep(ctx, task, "tiered decision step produced no LoopResult")
		w.failTieredDependents(ctx, task, parentTask)
		return
	}
	if !result.IsTerminalSuccess() {
		w.handleLoopResult(ctx, task, result)
		return
	}

	signal, err := w.readNeedsContextSignal(ctx, task)
	if err != nil {
		slog.Error("tiered decision: failed to read committed result", "task_id", task.ID, "error", err)
		w.failTieredOrigin(ctx, parentTask, "tiered decision: failed to read committed result")
		return
	}
	if signal.NeedsContext {
		if err := w.handleNeedsContext(ctx, task, parentTask, signal.Reason); err != nil {
			w.resolveFailedNeedsContext(ctx, task, parentTask, err)
		}
		return
	}
	w.reconcileBlockedDependents(ctx, task.ID)
}

// parseNeedsContextSignal scans raw, pre-commit LLM output for a
// needs-context sentinel ({"needs_context": true, "reason": "..."}), using
// the same JSON-candidate extraction as VerifyResult/ContextPack parsing so
// it tolerates markdown fences and surrounding prose. It requires a
// non-empty reason so an ordinary execute/verify artifact is never mistaken
// for a re-gather request (neither schema has a needs_context field, so
// there is no legitimate collision).
func parseNeedsContextSignal(output string) (needsContextSignal, bool) {
	for _, candidate := range extractJSONCandidates(output) {
		var signal needsContextSignal
		if err := json.Unmarshal([]byte(candidate), &signal); err != nil {
			continue
		}
		if signal.NeedsContext && strings.TrimSpace(signal.Reason) != "" {
			return signal, true
		}
	}
	return needsContextSignal{}, false
}

// taskDivertedToNeedsContext reports whether taskID's current state is
// NEEDS_CONTEXT, used to detect that interceptTieredNeedsContext already
// diverted a step before its caller's own post-commit read/classification
// logic runs.
func (w *Worker) taskDivertedToNeedsContext(ctx context.Context, taskID string) (bool, error) {
	current, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	return current.State == models.TaskStateNeedsContext, nil
}

// diversionCheckAttempts bounds the transient retries a verify step makes
// before giving up on learning whether it was diverted to NEEDS_CONTEXT.
const diversionCheckAttempts = 3

// confirmNotDiverted reports whether taskID is definitely not parked in
// NEEDS_CONTEXT, retrying transient store failures. The verify flow must not
// classify output until this returns true: a diverted step has no committed
// RESULT event, so reading one would report a parse failure and route a
// verify failure for output that was never a verify result.
func (w *Worker) confirmNotDiverted(ctx context.Context, taskID string) (bool, error) {
	var err error
	for attempt := 1; attempt <= diversionCheckAttempts; attempt++ {
		var diverted bool
		if diverted, err = w.taskDivertedToNeedsContext(ctx, taskID); err == nil {
			return !diverted, nil
		}
		if attempt < diversionCheckAttempts {
			slog.Warn("tiered verify: retrying NEEDS_CONTEXT diversion check",
				"task_id", taskID, "attempt", attempt, "error", err)
		}
	}
	return false, err
}

// interceptTieredNeedsContext intercepts a tiered execute/verify step's
// output before the engine's unconditional commit (T-023). Decision detects
// NEEDS_CONTEXT after commit (readNeedsContextSignal), which is safe there
// because committing only marks the decision itself COMPLETED before
// tiered code judges that already-committed answer. For execute/verify,
// committing first would unconditionally unlock any BLOCKED dependents in
// the same transaction (finishTaskResultSideEffects), racing whoever picks
// them up next against this NEEDS_CONTEXT decision. Intercepting here,
// while the task is still RUNNING, transitions straight to NEEDS_CONTEXT
// (a valid edge in models.validTaskTransitions) and never calls
// store.UpdateTaskResult, so dependents are never unlocked in the first
// place. Returns true when it fully handled the commit (caller must not
// fall through to the normal success-commit path).
func (w *Worker) interceptTieredNeedsContext(ctx context.Context, task models.Task, content string) bool {
	if !w.isTieredStep(task) {
		return false
	}
	kind := w.tieredStepKind(task)
	if kind != TieredStepExecute && kind != TieredStepVerify {
		return false
	}
	signal, ok := parseNeedsContextSignal(content)
	if !ok {
		return false
	}
	parents, err := w.store.ListParentTasksByRelation(ctx, task.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		slog.Error("tiered: failed to look up SPAWNED_BY parent for pre-commit NEEDS_CONTEXT", "task_id", task.ID, "step", kind, "error", err)
		w.FailHard(ctx, task, fmt.Errorf("tiered NEEDS_CONTEXT: parent lookup failed: %w", err))
		return true
	}
	if len(parents) == 0 {
		slog.Error("tiered step has no SPAWNED_BY origin parent", "task_id", task.ID, "agent_id", task.AgentID)
		w.FailHard(ctx, task, fmt.Errorf("tiered NEEDS_CONTEXT: task %s has no SPAWNED_BY origin parent", task.ID))
		return true
	}
	if err := w.handleNeedsContext(ctx, task, parents[0], signal.Reason); err != nil {
		w.resolveFailedNeedsContext(ctx, task, parents[0], err)
	}
	return true
}

// resolveFailedNeedsContext gives a diverted step a terminal, visible state
// after its re-gather failed. handleNeedsContext transitions the step to
// NEEDS_CONTEXT first and can then fail on the generation lookup, the
// replacement chain spawn, or the dependent rewire, leaving a step with no
// committed result and dependents that may never be unblocked.
//
// The replacement chain is spawned under an idempotency key derived from the
// origin and generation, so retrying handleNeedsContext for the same
// generation reuses a partially spawned chain instead of duplicating it. A
// step that never left RUNNING is therefore requeued to retry the whole
// diversion; one already parked in NEEDS_CONTEXT is moved to FAILED and its
// dependents (and the origin) resolved, so the pipeline cannot hang.
func (w *Worker) resolveFailedNeedsContext(ctx context.Context, task, parentTask models.Task, cause error) {
	slog.Error("tiered: needs-context rewire failed", "task_id", task.ID, "error", cause)
	w.Emit(ctx, task, "TIERED_NEEDS_CONTEXT_ERROR", cause.Error())
	current, err := w.store.GetTask(ctx, task.ID)
	if err != nil {
		slog.Error("tiered: failed to re-read step after needs-context failure", "task_id", task.ID, "error", err)
		return
	}
	if current.State == models.TaskStateRunning {
		w.requeue(ctx, *current, "needs-context re-gather failed: "+cause.Error())
		return
	}
	if current.State != models.TaskStateNeedsContext {
		return
	}
	if _, err := w.store.UpdateTaskState(ctx, current.ID, current.UpdatedAt, models.TaskStateFailed); err != nil {
		slog.Error("tiered: failed to abandon a step whose re-gather could not be scheduled",
			"task_id", current.ID, "error", err)
		return
	}
	w.Emit(ctx, task, "TIERED_NEEDS_CONTEXT_ABANDONED", cause.Error())
	w.failTieredDependents(ctx, *current, parentTask)
}

func (w *Worker) readNeedsContextSignal(ctx context.Context, task models.Task) (needsContextSignal, error) {
	payload, err := w.readCommittedText(ctx, task.ID)
	if err != nil {
		return needsContextSignal{}, err
	}
	var signal needsContextSignal
	clean := strings.TrimSpace(payload)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(strings.TrimSpace(clean), "```")
	if err := json.Unmarshal([]byte(strings.TrimSpace(clean)), &signal); err != nil {
		return needsContextSignal{}, fmt.Errorf("decode needs-context signal: %w", err)
	}
	if signal.NeedsContext {
		if strings.TrimSpace(signal.Reason) == "" {
			return needsContextSignal{}, fmt.Errorf("needs-context signal requires a reason")
		}
		return signal, nil
	}
	var decision struct {
		TouchList []string `json:"touch_list"`
		Checks    []string `json:"checks"`
		Rationale string   `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(clean)), &decision); err != nil ||
		decision.TouchList == nil || decision.Checks == nil || strings.TrimSpace(decision.Rationale) == "" {
		return needsContextSignal{}, fmt.Errorf("invalid tiered decision: expected touch_list, checks, and rationale")
	}
	return signal, nil
}

// reconcileBlockedDependents re-readies BLOCKED children of completedTaskID.
func (w *Worker) reconcileBlockedDependents(ctx context.Context, completedTaskID string) {
	children, err := w.store.ListChildTasksByRelation(ctx, completedTaskID, models.TaskRelationDependsOn)
	if err != nil {
		slog.Error("tiered: failed to list depends_on children for reconcile", "task_id", completedTaskID, "error", err)
		return
	}
	for _, child := range children {
		if child.State != models.TaskStateBlocked {
			continue
		}
		if !w.allDependenciesResolved(ctx, child.ID) {
			continue
		}
		if _, err := w.store.UpdateTaskState(ctx, child.ID, child.UpdatedAt, models.TaskStateReady); err != nil {
			slog.Error("tiered: failed to re-ready blocked dependent", "task_id", child.ID, "error", err)
		}
	}
}

func (w *Worker) allDependenciesResolved(ctx context.Context, taskID string) bool {
	depParents, err := w.store.ListParentTasksByRelation(ctx, taskID, models.TaskRelationDependsOn)
	if err != nil {
		return false
	}
	blockParents, err := w.store.ListParentTasksByRelation(ctx, taskID, models.TaskRelationBlocks)
	if err != nil {
		return false
	}
	for _, p := range append(depParents, blockParents...) {
		if p.State != models.TaskStateCompleted {
			return false
		}
	}
	return true
}

func (w *Worker) handleNeedsContextDep(ctx context.Context, task models.Task, dependency models.Task, origin models.Task) bool {
	freshDecisionID, err := w.freshDecisionID(ctx, origin.ID, dependency.ID)
	if err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision lookup failed: %w", err))
		return true
	}
	if freshDecisionID != "" {
		fresh, err := w.store.GetTask(ctx, freshDecisionID)
		if err != nil {
			w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision fetch failed: %w", err))
			return true
		}
		return w.handleFreshDecision(ctx, task, dependency, fresh)
	}
	return w.handleStaleDecision(ctx, task, dependency, origin)
}
