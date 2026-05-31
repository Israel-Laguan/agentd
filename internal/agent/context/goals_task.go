package context

import (
	"strings"

	"agentd/internal/models"
)

// parseGoalProgress extracts completed and blocked criteria markers from the
// model's response text. The model is expected to emit lines like:
//
//	[COMPLETED] criterion text
//	[BLOCKED] criterion text
func ParseGoalProgress(content string) (completed, blocked []string) {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, marker := range []string{"[COMPLETED]", "[BLOCKED]"} {
			if len(trimmed) < len(marker) || !strings.EqualFold(trimmed[:len(marker)], marker) {
				continue
			}
			v := strings.TrimSpace(trimmed[len(marker):])
			if v == "" {
				continue
			}
			if marker == "[COMPLETED]" {
				completed = append(completed, v)
			} else {
				blocked = append(blocked, v)
			}
			break
		}
	}
	return completed, blocked
}

func parseGoalProgress(content string) (completed, blocked []string) {
	return ParseGoalProgress(content)
}

// GoalFromTask extracts a goal from a task's SuccessCriteria and
// description. Returns nil when no success criteria are present.
func GoalFromTask(task models.Task) *AgentGoal {
	if len(task.SuccessCriteria) == 0 {
		return nil
	}
	allowed, criteria := normalizeSuccessCriteria(task.SuccessCriteria)
	if len(criteria) == 0 {
		return nil
	}
	return &AgentGoal{
		Description:       task.Description,
		SuccessCriteria:   criteria,
		CompletedCriteria: persistedCriteriaMet(allowed, task.CriteriaMet),
	}
}

func normalizeSuccessCriteria(raw []string) (map[string]struct{}, []string) {
	seen := make(map[string]struct{}, len(raw))
	criteria := make([]string, 0, len(raw))
	for _, c := range raw {
		v := strings.TrimSpace(c)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		criteria = append(criteria, v)
	}
	return seen, criteria
}

func persistedCriteriaMet(allowed map[string]struct{}, stored []string) []string {
	met := make([]string, 0, len(stored))
	seenMet := make(map[string]struct{}, len(stored))
	for _, c := range stored {
		v := strings.TrimSpace(c)
		if v == "" {
			continue
		}
		if _, ok := allowed[v]; !ok {
			continue
		}
		if _, ok := seenMet[v]; ok {
			continue
		}
		seenMet[v] = struct{}{}
		met = append(met, v)
	}
	return met
}
