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

// needsContextSignal is the alternative Decision-step output that flags an
// insufficient ContextPack instead of a normal touch_list/checks plan. See
// decisionStepPrompt.
type needsContextSignal struct {
	NeedsContext bool   `json:"needs_context"`
	Reason       string `json:"reason"`
}

// readCommittedText reads the most recent RESULT event for a task and
// strips the "exit=%d duration=%s\n" prefix commitSucceeded adds, leaving
// the model's raw final text (see readVerifyResult for the tiered verify
// use of this same shape).
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

// processTieredDecisionStep runs the decision step, then checks its
// committed output for the NEEDS_CONTEXT sentinel before falling through to
// the normal reconcile pass. NEEDS_CONTEXT is deliberately detected here,
// post-commit, rather than treated as a special LLM response type: the
// decision step already completed nominally (the engine commits
// unconditionally once the model answers), and only afterward is that
// answer judged to have come from an insufficient pack.
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
	// Not every decision output is the sentinel shape — a normal
	// touch_list/checks Decision artifact fails this unmarshal, which is
	// the expected, common case, not an error.
	clean := strings.TrimSpace(payload)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(strings.TrimSpace(clean), "```")
	_ = json.Unmarshal([]byte(strings.TrimSpace(clean)), &signal)
	return signal, nil
}

// nextContextPackGeneration counts existing context-step children already
// spawned for this pipeline's origin (the DAG's original context step plus
// any prior re-gathers) to derive the next generation number.
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

// handleNeedsContext implements the NEEDS_CONTEXT re-gather: it moves the
// task that flagged insufficient context into NEEDS_CONTEXT, spawns a fresh
// context→decision continuation, and rewires the stale decision's
// dependents onto the new decision so they pick up the re-gathered pack
// once it is ready.
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

	assignee := parentTask.Assignee
	if !assignee.Valid() {
		assignee = models.TaskAssigneeSystem
	}

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

// regatherPair holds the IDs of a freshly spawned re-gather chain.
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

// reconcileBlockedDependents re-readies any child of completedTaskID that
// RewireDependsOn had to park in BLOCKED, once all of that child's
// DEPENDS_ON/BLOCKS parents (now including the freshly rewired one) are
// COMPLETED. UnlockReadyChildren (the store's own completion side effect)
// only promotes PENDING tasks, so a BLOCKED dependent needs this explicit
// pass — see the KanbanStore.RewireDependsOn doc for why READY dependents
// are demoted to BLOCKED in the first place.
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
