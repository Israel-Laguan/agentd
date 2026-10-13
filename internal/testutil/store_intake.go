package testutil

import (
	"context"

	"agentd/internal/models"
)

// Comment-intake tracking for FakeKanbanStore. These methods mirror
// internal/kanban/intake_query.go so intake tests exercise the same listing
// and dedupe semantics as the SQLite store without importing it.

// ListUnprocessedHumanComments mirrors kanban/intake_query.go: tasks flagged
// HUMAN in IN_CONSIDERATION or a terminal state, with human-authored comments
// not yet marked processed.
func (s *FakeKanbanStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.CommentRef
	for _, stored := range s.comments {
		c := decodeCommentPayload(stored)
		if c.Author != models.CommentAuthorUser {
			continue
		}
		t, ok := s.tasks[c.TaskID]
		if !ok {
			continue
		}
		if t.Assignee != models.TaskAssigneeHuman {
			continue
		}
		if t.State != models.TaskStateInConsideration && !t.State.Terminal() {
			continue
		}
		if _, done := s.processedComments[processedCommentKey(c.TaskID, stored.ID)]; done {
			continue
		}
		out = append(out, models.CommentRef{
			TaskID:         c.TaskID,
			CommentEventID: stored.ID,
			Body:           c.Body,
			UpdatedAt:      stored.CreatedAt,
		})
	}
	return out, nil
}

func processedCommentKey(taskID, commentEventID string) string {
	return taskID + "\x00" + commentEventID
}

// MarkCommentProcessed records intake consumption and releases the task back
// to the system assignee, mirroring the SQLite COMMENT_INTAKE row plus
// assignee flip (state is untouched in both stores).
func (s *FakeKanbanStore) MarkCommentProcessed(_ context.Context, taskID, commentEventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processedComments == nil {
		s.processedComments = make(map[string]struct{})
	}
	s.processedComments[processedCommentKey(taskID, commentEventID)] = struct{}{}
	if t, ok := s.tasks[taskID]; ok {
		t.Assignee = models.TaskAssigneeSystem
		t.UpdatedAt = now()
		s.tasks[taskID] = t
	}
	return nil
}
