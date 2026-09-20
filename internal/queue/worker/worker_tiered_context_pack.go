package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

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