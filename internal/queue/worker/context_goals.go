package worker

import "strings"

// SetGoalTracker attaches a GoalTracker so compression can prioritise
// turns based on goal progress (completed-criteria turns compress first,
// blocked-criteria turns are retained in the working zone).
func (cm *ContextManager) SetGoalTracker(gt *GoalTracker) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.goalTracker = gt
}

// goalAwarePartition reorders the boundary between compressed and working
// turns based on goal progress. Turns that only mention completed criteria
// are pushed into the compressed set, while turns mentioning blocked
// criteria are pulled into the working set for retention. When no
// GoalTracker is attached, the original split is returned unchanged.
func (cm *ContextManager) goalAwarePartition(compressed, working []Turn) ([]Turn, []Turn) {
	if cm.goalTracker == nil {
		return compressed, working
	}
	goal := cm.goalTracker.Goal()
	if goal == nil || len(goal.SuccessCriteria) == 0 {
		return compressed, working
	}

	blockedSet := make(map[string]struct{}, len(goal.BlockedCriteria))
	for _, c := range goal.BlockedCriteria {
		blockedSet[c] = struct{}{}
	}
	completedSet := make(map[string]struct{}, len(goal.CompletedCriteria))
	for _, c := range goal.CompletedCriteria {
		completedSet[c] = struct{}{}
	}

	var retainFromCompressed []Turn
	var keepCompressed []Turn
	for _, t := range compressed {
		if turnMentionsCriteria(t, blockedSet) {
			retainFromCompressed = append(retainFromCompressed, t)
		} else {
			keepCompressed = append(keepCompressed, t)
		}
	}

	var promoteToCompressed []Turn
	var keepWorking []Turn
	for _, t := range working {
		if turnMentionsOnlyCompleted(t, completedSet, blockedSet) {
			promoteToCompressed = append(promoteToCompressed, t)
		} else {
			keepWorking = append(keepWorking, t)
		}
	}

	finalCompressed := append(keepCompressed, promoteToCompressed...)
	finalWorking := append(retainFromCompressed, keepWorking...)
	return finalCompressed, finalWorking
}

// turnMentionsCriteria checks whether any message in the turn references
// one of the criteria in the set.
func turnMentionsCriteria(t Turn, criteria map[string]struct{}) bool {
	for _, m := range t.Messages {
		for c := range criteria {
			if strings.Contains(m.Content, c) {
				return true
			}
		}
	}
	return false
}

// turnMentionsOnlyCompleted returns true when a turn mentions at least
// one completed criterion and no blocked criteria.
func turnMentionsOnlyCompleted(t Turn, completed, blocked map[string]struct{}) bool {
	hasCompleted := false
	for _, m := range t.Messages {
		for c := range blocked {
			if strings.Contains(m.Content, c) {
				return false
			}
		}
		for c := range completed {
			if strings.Contains(m.Content, c) {
				hasCompleted = true
			}
		}
	}
	return hasCompleted
}
