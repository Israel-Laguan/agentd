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
	cm.mu.Lock()
	goalTracker := cm.goalTracker
	cm.mu.Unlock()
	if goalTracker == nil {
		return compressed, working
	}
	goal := goalTracker.Goal()
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

func (cm *ContextManager) reconcileSummaryState(compressedTurns []Turn) {
	current := make(map[uint64]struct{}, len(compressedTurns))
	for _, t := range compressedTurns {
		current[cm.hashTurn(t)] = struct{}{}
	}

	cm.cacheMu.Lock()
	defer cm.cacheMu.Unlock()
	if len(current) == 0 {
		cm.summarizedTurns = make(map[uint64]bool)
		cm.runningSummary = nil
		return
	}
	for hash := range cm.summarizedTurns {
		if _, ok := current[hash]; !ok {
			cm.summarizedTurns = make(map[uint64]bool)
			cm.runningSummary = nil
			return
		}
	}
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
