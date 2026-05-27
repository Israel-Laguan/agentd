package testutil

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
