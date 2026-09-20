package testutil

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"agentd/internal/models"
)

// unlockReadyChildrenLocked mirrors UnlockReadyChildren for the fake: a
// PENDING child whose blocking parents are all COMPLETED moves to READY.
// Only BLOCKS/DEPENDS_ON edges gate readiness — SPAWNED_BY provenance edges
// are ignored, matching the real query's relation_type filter.
func (s *FakeKanbanStore) unlockReadyChildrenLocked(parentID string, ts time.Time) {
	for _, childID := range s.listBlockingChildTaskIDsLocked(parentID) {
		child, ok := s.tasks[childID]
		if !ok || child.State != models.TaskStatePending {
			continue
		}
		ready := true
		for _, rel := range s.childParentRelations[childID] {
			if rel.relationType != models.TaskRelationBlocks && rel.relationType != models.TaskRelationDependsOn {
				continue
			}
			parent, ok := s.tasks[rel.parentID]
			if !ok {
				continue
			}
			if parent.State != models.TaskStateCompleted {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		child.State = models.TaskStateReady
		child.UpdatedAt = ts
		s.tasks[childID] = child
	}
}

// CompleteTieredOrigin mirrors the real store's atomic tiered-origin
// completion (see kanban.Store.CompleteTieredOrigin): a BLOCKED, READY, or
// RUNNING origin flips straight to COMPLETED/FAILED under a single lock
// hold, guarded by the caller's expected UpdatedAt. A stale version or a
// task already in a terminal state reports ErrStateConflict, and no
// intermediate READY state is ever exposed for another dispatcher to claim.
func (s *FakeKanbanStore) CompleteTieredOrigin(_ context.Context, id string, expectedUpdatedAt time.Time, result models.TaskResult) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		// Mirror the real guarded UPDATE: zero rows affected reports
		// ErrStateConflict whether the id is unknown or the version moved.
		return nil, models.ErrStateConflict
	}
	if !t.UpdatedAt.Equal(expectedUpdatedAt) {
		return nil, models.ErrStateConflict
	}
	switch t.State {
	case models.TaskStateBlocked, models.TaskStateReady, models.TaskStateRunning:
	default:
		return nil, models.ErrStateConflict
	}
	ts := now()
	// Guarantee the version moves forward even if the clock has not ticked
	// past the caller's read, mirroring kanban.Store.CompleteTieredOrigin.
	if !ts.After(expectedUpdatedAt) {
		ts = expectedUpdatedAt.Add(time.Nanosecond)
	}
	if result.Success {
		t.State = models.TaskStateCompleted
	} else {
		t.State = models.TaskStateFailed
	}
	t.CompletedAt = &ts
	t.UpdatedAt = ts
	s.tasks[id] = t
	if result.Success {
		s.unlockReadyChildrenLocked(id, ts)
	}
	s.unblockBlockedParentsLocked(id)
	if strings.TrimSpace(result.Payload) != "" {
		s.events = append(s.events, models.Event{
			BaseEntity: models.BaseEntity{ID: s.nextID(), CreatedAt: ts, UpdatedAt: ts},
			ProjectID:  t.ProjectID,
			TaskID:     sql.NullString{String: id, Valid: true},
			Type:       models.EventTypeResult,
			Payload:    result.Payload,
		})
	}
	return &t, nil
}
