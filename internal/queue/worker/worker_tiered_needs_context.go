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
			slog.Error("tiered decision: needs-context rewire failed", "task_id", task.ID, "error", err)
			w.Emit(ctx, task, "TIERED_NEEDS_CONTEXT_ERROR", err.Error())
		}
		return
	}
	w.reconcileBlockedDependents(ctx, task.ID)
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
	return signal, nil
}

// nextContextPackGeneration derives the next generation number.
func (w *Worker) nextContextPackGeneration(ctx context.Context, originID string) (int, error) {
	children, err := w.store.ListChildTasksByRelation(ctx, originID, models.TaskRelationSpawnedBy)
	if err != nil {
		return 0, fmt.Errorf("list spawned children: %w", err)
	}
	count := 0
	for _, child := range children {
		if child.AgentID == tieredStepProfile[TieredStepContext] {
			count++
		}
	}
	return count + 1, nil
}

// handleNeedsContext implements the NEEDS_CONTEXT re-gather.
func (w *Worker) handleNeedsContext(ctx context.Context, task models.Task, parentTask models.Task, reason string) error {
	current, err := w.store.GetTask(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("reload task before NEEDS_CONTEXT transition: %w", err)
	}
	if _, err := w.store.UpdateTaskState(ctx, current.ID, current.UpdatedAt, models.TaskStateNeedsContext); err != nil {
		return fmt.Errorf("transition to NEEDS_CONTEXT: %w", err)
	}
	w.Emit(ctx, task, "TIERED_NEEDS_CONTEXT", reason)

	generation, err := w.nextContextPackGeneration(ctx, parentTask.ID)
	if err != nil {
		return fmt.Errorf("determine pack generation: %w", err)
	}
	origin, err := w.store.GetTask(ctx, parentTask.ID)
	if err != nil {
		return fmt.Errorf("reload origin before re-gather: %w", err)
	}
	if origin.State == models.TaskStateFailed || origin.State == models.TaskStateFailedRequiresHuman {
		return fmt.Errorf("origin is %s; skipping re-gather", origin.State)
	}
	if generation > w.tieredCfg.EscalationConfig().MaxReGather {
		if _, err := w.store.UpdateTaskState(ctx, parentTask.ID, origin.UpdatedAt, models.TaskStateFailedRequiresHuman); err != nil {
			return fmt.Errorf("hand off after re-gather cap: %w", err)
		}
		w.Emit(ctx, parentTask, "TIERED_NEEDS_CONTEXT_CAP", fmt.Sprintf("max re-gathers reached: %d", generation-1))
		return nil
	}

	assignee := w.resolveAssignee(parentTask.Assignee)
	pair, err := w.spawnContextPackChain(ctx, parentTask, assignee, generation)
	if err != nil {
		return fmt.Errorf("spawn re-gather context/decision chain: %w", err)
	}
	rewired, err := w.store.RewireDependsOn(ctx, task.ID, pair.decisionID)
	if err != nil {
		return fmt.Errorf("rewire downstream dependents: %w", err)
	}
	w.Emit(ctx, task, "TIERED_PACK_REWIRED", fmt.Sprintf("generation=%d rewired=%d", generation, len(rewired)))
	return nil
}

func (w *Worker) resolveAssignee(a models.TaskAssignee) models.TaskAssignee {
	if !a.Valid() {
		return models.TaskAssigneeSystem
	}
	return a
}

type regatherPair struct {
	contextID  string
	decisionID string
}

func (w *Worker) spawnContextPackChain(ctx context.Context, parentTask models.Task, assignee models.TaskAssignee, generation int) (regatherPair, error) {
	now := time.Now().UTC()
	newContext := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   parentTask.ProjectID,
		AgentID:     tieredStepProfile[TieredStepContext],
		Title:       fmt.Sprintf("context (re-gather v%d): %s", generation, parentTask.Title),
		Description: parentTask.Description,
		State:       models.TaskStateReady,
		Assignee:    assignee,
	}
	newDecision := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   parentTask.ProjectID,
		AgentID:     tieredStepProfile[TieredStepDecision],
		Title:       fmt.Sprintf("decision (re-gather v%d): %s", generation, parentTask.Title),
		Description: parentTask.Description,
		State:       models.TaskStatePending,
		Assignee:    assignee,
	}

	if _, err := w.store.SpawnTieredContinuation(ctx, parentTask.ID, []models.TieredContinuationTask{
		{Task: newContext},
		{Task: newDecision, DependsOnID: newContext.ID},
	}); err != nil {
		return regatherPair{}, err
	}
	return regatherPair{contextID: newContext.ID, decisionID: newDecision.ID}, nil
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

func (w *Worker) handleFreshDecision(ctx context.Context, task models.Task, dependency models.Task, fresh *models.Task) bool {
	switch fresh.State {
	case models.TaskStateCompleted:
		return w.rewireToCompletedDecision(ctx, task, dependency, fresh.ID)
	case models.TaskStateFailed, models.TaskStateFailedRequiresHuman:
		if _, err := w.store.RewireDependsOn(ctx, dependency.ID, fresh.ID); err != nil {
			w.FailHard(ctx, task, fmt.Errorf("tiered rewire to failed decision failed: %w", err))
			return true
		}
		w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", fresh.ID, fresh.State))
		return true
	case models.TaskStateNeedsContext:
		if task.State.CanTransitionTo(models.TaskStateBlocked) {
			if _, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
				w.FailHard(ctx, task, fmt.Errorf("tiered park while awaiting fresh context failed: %w", err))
			}
		}
		return true
	default:
		if task.State.CanTransitionTo(models.TaskStateBlocked) {
			if _, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
				w.FailHard(ctx, task, fmt.Errorf("tiered park before dependency rewire failed: %w", err))
				return true
			}
		}
		if _, err := w.store.RewireDependsOn(ctx, dependency.ID, fresh.ID); err != nil {
			w.FailHard(ctx, task, fmt.Errorf("tiered rewire to fresh decision failed: %w", err))
			return true
		}
		return true
	}
}

func (w *Worker) handleStaleDecision(ctx context.Context, task models.Task, dependency models.Task, origin models.Task) bool {
	completedID, err := w.freshTerminalDecisionID(ctx, origin.ID, dependency.ID, models.TaskStateCompleted)
	if err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered completed decision lookup failed: %w", err))
		return true
	}
	if completedID != "" {
		return w.rewireToCompletedDecision(ctx, task, dependency, completedID)
	}
	failedID, err := w.freshTerminalDecisionID(ctx, origin.ID, dependency.ID, models.TaskStateFailed)
	if err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered failed decision lookup failed: %w", err))
		return true
	}
	if failedID != "" {
		_, _ = w.store.RewireDependsOn(ctx, dependency.ID, failedID)
		w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", failedID, models.TaskStateFailed))
		return true
	}
	failedHumanID, err := w.freshTerminalDecisionID(ctx, origin.ID, dependency.ID, models.TaskStateFailedRequiresHuman)
	if err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered human-failed decision lookup failed: %w", err))
		return true
	}
	if failedHumanID != "" {
		_, _ = w.store.RewireDependsOn(ctx, dependency.ID, failedHumanID)
		w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", failedHumanID, models.TaskStateFailedRequiresHuman))
		return true
	}
	w.FailHard(ctx, task, fmt.Errorf("tiered step depends on stale decision %s awaiting context re-gather", dependency.ID))
	return true
}

func (w *Worker) rewireToCompletedDecision(ctx context.Context, task models.Task, dependency models.Task, decisionID string) bool {
	if _, err := w.store.RewireDependsOn(ctx, dependency.ID, decisionID); err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered rewire to completed decision failed: %w", err))
		return true
	}
	if w.allDependenciesResolved(ctx, task.ID) {
		latest, err := w.store.GetTask(ctx, task.ID)
		if err == nil && latest.State.CanTransitionTo(models.TaskStateReady) {
			if _, err := w.store.UpdateTaskState(ctx, latest.ID, latest.UpdatedAt, models.TaskStateReady); err != nil {
				slog.Error("tiered: failed to re-ready after completed fresh decision", "task_id", latest.ID, "error", err)
			}
		}
	}
	return true
}
