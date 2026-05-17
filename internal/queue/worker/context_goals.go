package worker

import (
	"regexp"
	"strings"
)

// SetGoalTracker attaches a GoalTracker so compression can expand the
// working suffix to retain turns that mention blocked criteria.
func (cm *ContextManager) SetGoalTracker(gt *GoalTracker) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.goalTracker = gt
}

// goalAwarePartition adjusts the split between compressed and working
// turns while preserving chronological order. When blocked criteria exist,
// the split moves earlier so turns mentioning blocked criteria remain in
// the working (unsummarized) suffix. Turns are never moved across zones.
func (cm *ContextManager) goalAwarePartition(turns []Turn, defaultSplitIdx int) ([]Turn, []Turn) {
	if defaultSplitIdx <= 0 {
		return nil, turns
	}
	if defaultSplitIdx >= len(turns) {
		return turns, nil
	}

	cm.mu.Lock()
	goalTracker := cm.goalTracker
	cm.mu.Unlock()
	if goalTracker == nil {
		return turns[:defaultSplitIdx], turns[defaultSplitIdx:]
	}
	goal := goalTracker.Goal()
	if goal == nil || len(goal.BlockedCriteria) == 0 {
		return turns[:defaultSplitIdx], turns[defaultSplitIdx:]
	}

	blockedSet := make(map[string]struct{}, len(goal.BlockedCriteria))
	for _, c := range goal.BlockedCriteria {
		blockedSet[c] = struct{}{}
	}

	splitIdx := defaultSplitIdx
	for splitIdx > 0 && turnMentionsCriteria(turns[splitIdx-1], blockedSet) {
		splitIdx--
	}
	return turns[:splitIdx], turns[splitIdx:]
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

// messageMentionsCriterion reports whether content references a criterion
// via goal-progress markers or as a whole word/phrase.
func messageMentionsCriterion(content, criterion string) bool {
	criterion = strings.TrimSpace(criterion)
	if criterion == "" {
		return false
	}
	lower := strings.ToLower(content)
	for _, prefix := range []string{"[completed] ", "[blocked] "} {
		if idx := strings.Index(lower, prefix); idx >= 0 {
			after := strings.TrimSpace(content[idx+len(prefix):])
			if len(after) >= len(criterion) {
				lineEnd := strings.IndexByte(after, '\n')
				if lineEnd < 0 {
					lineEnd = len(after)
				}
				line := strings.TrimSpace(after[:lineEnd])
				if strings.EqualFold(line, criterion) {
					return true
				}
			}
		}
	}
	pattern := `(?i)\b` + regexp.QuoteMeta(criterion) + `\b`
	matched, err := regexp.MatchString(pattern, content)
	return err == nil && matched
}

// turnMentionsCriteria checks whether any message in the turn references
// one of the criteria in the set.
func turnMentionsCriteria(t Turn, criteria map[string]struct{}) bool {
	for _, m := range t.Messages {
		for c := range criteria {
			if messageMentionsCriterion(m.Content, c) {
				return true
			}
		}
	}
	return false
}
