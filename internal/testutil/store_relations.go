package testutil

import "agentd/internal/models"

// listChildTaskIDsLocked returns child task IDs whose childParents entry includes parentID.
func (s *FakeKanbanStore) listChildTaskIDsLocked(parentID string) []string {
	var ids []string
	for childID, parents := range s.childParents {
		for _, pid := range parents {
			if pid == parentID {
				ids = append(ids, childID)
				break
			}
		}
	}
	return ids
}

// listBlockingChildTaskIDsLocked returns child task IDs linked to parentID via
// a BLOCKS or DEPENDS_ON edge, mirroring the real UnlockReadyChildren query's
// relation_type filter. SPAWNED_BY provenance edges are not blocking
// prerequisites and are excluded.
func (s *FakeKanbanStore) listBlockingChildTaskIDsLocked(parentID string) []string {
	var ids []string
	for childID, rels := range s.childParentRelations {
		for _, rel := range rels {
			if rel.parentID != parentID {
				continue
			}
			if rel.relationType != models.TaskRelationBlocks && rel.relationType != models.TaskRelationDependsOn {
				continue
			}
			ids = append(ids, childID)
			break
		}
	}
	return ids
}
