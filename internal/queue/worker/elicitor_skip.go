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
	// Match imperative/spec language, not casual narrative ("as expected", "should we").
	elicitationConstraintKeywords = regexp.MustCompile(`(?i)(?:` +
		`\bmust\s+(?:not\s+)?\w+` +
		`|\bshould\s+not\b|\bshall\s+not\b` +
		`|\bacceptance\s*(?:criteria|:)` +
		`|\brepro(?:duction)?\s+steps?\b|\bsteps?\s+to\s+repro(?:duce)?\b` +
		`|\bexpected\s+(?:behavior|result|outcome|output|response)\b` +
		`|\bconstraints?\s*:` +
		`|\brequirements?\s*:` +
		`)`)
	elicitationListPattern     = regexp.MustCompile(`(?m)^\s*([-*•]|\d+[.)])\s+\S`)
	elicitationFilePathPattern = regexp.MustCompile(`(?:^|[\s(])(?:[\w.-]+/)*[\w.-]+\.(?:go|ts|tsx|js|jsx|py|rs|java|md|yaml|yml|json)\b`)
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
	// Multi-section specs usually have several paragraph breaks; avoid skipping long prose.
	if strings.Count(desc, "\n") >= 5 {
		return true
	}
	return false
}
