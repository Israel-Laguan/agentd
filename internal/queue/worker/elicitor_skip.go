package worker

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"agentd/internal/models"
)

func shouldSkipElicitation(task models.Task) bool {
	desc := task.Description
	if strings.Contains(desc, clarificationsBlockHeader) {
		return true
	}
	if utf8.RuneCountInString(strings.TrimSpace(desc)) > elicitationSkipMinDescriptionRunes && hasExplicitConstraints(task) {
		return true
	}
	return false
}

var (
	elicitationConstraintKeywords = regexp.MustCompile(`(?i)\b(must|should not|shall not|acceptance|repro|reproduce|expected|constraint|requirements?)\b`)
	elicitationListPattern        = regexp.MustCompile(`(?m)^\s*([-*•]|\d+[.)])\s+\S`)
	elicitationFilePathPattern    = regexp.MustCompile(`\b[\w./-]+\.(go|ts|tsx|js|jsx|py|rs|java|md|yaml|yml|json)\b`)
)

func hasExplicitConstraints(task models.Task) bool {
	if len(task.SuccessCriteria) > 0 {
		return true
	}
	desc := task.Description
	if elicitationListPattern.MatchString(desc) {
		return true
	}
	if elicitationConstraintKeywords.MatchString(desc) {
		return true
	}
	if elicitationFilePathPattern.MatchString(desc) {
		return true
	}
	if strings.Count(desc, "\n") >= 3 {
		return true
	}
	return false
}
