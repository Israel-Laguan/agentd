package testutil

import (
	"strings"

	"agentd/internal/models"
)

func resolveTaskAgentID(draftAgentID string) string {
	if id := strings.TrimSpace(draftAgentID); id != "" {
		return id
	}
	return "default"
}

func (s *FakeKanbanStore) validateDraftAgentIDs(drafts []models.DraftTask) error {
	seen := make(map[string]struct{})
	for _, draft := range drafts {
		id := resolveTaskAgentID(draft.AgentID)
		if id == "default" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if _, ok := s.profiles[id]; !ok {
			return models.ErrAgentProfileNotFound
		}
		seen[id] = struct{}{}
	}
	return nil
}
