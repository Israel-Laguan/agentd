package worker

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (w *Worker) freshDecisionID(ctx context.Context, originID, staleDecisionID string) (string, error) {
	stale, err := w.store.GetTask(ctx, staleDecisionID)
	if err != nil {
		return "", fmt.Errorf("get stale decision: %w", err)
	}
	children, err := w.store.ListChildTasksByRelation(ctx, originID, models.TaskRelationSpawnedBy)
	if err != nil {
		return "", fmt.Errorf("list spawned children: %w", err)
	}
	var id string
	var latest time.Time
	for _, child := range children {
		if child.AgentID != tieredStepProfile[TieredStepDecision] || child.State == models.TaskStateCompleted || child.State == models.TaskStateFailed || child.State == models.TaskStateFailedRequiresHuman || child.State == models.TaskStateNeedsContext {
			continue
		}
		if child.CreatedAt.After(stale.CreatedAt) && child.CreatedAt.After(latest) {
			id, latest = child.ID, child.CreatedAt
		}
	}
	return id, nil
}

func (w *Worker) freshTerminalDecisionID(ctx context.Context, originID, staleDecisionID string, terminalState models.TaskState) (string, error) {
	stale, err := w.store.GetTask(ctx, staleDecisionID)
	if err != nil {
		return "", fmt.Errorf("get stale decision: %w", err)
	}
	children, err := w.store.ListChildTasksByRelation(ctx, originID, models.TaskRelationSpawnedBy)
	if err != nil {
		return "", fmt.Errorf("list spawned children: %w", err)
	}
	var id string
	var latest time.Time
	for _, child := range children {
		if child.AgentID != tieredStepProfile[TieredStepDecision] || child.State != terminalState {
			continue
		}
		if child.CreatedAt.After(stale.CreatedAt) && child.CreatedAt.After(latest) {
			id, latest = child.ID, child.CreatedAt
		}
	}
	return id, nil
}
