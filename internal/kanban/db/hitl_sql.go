package db

import (
	"fmt"
	"strings"

	"agentd/internal/models"
)

// ChildResolvedConditionSQL returns SQL for whether a child is resolved for
// parent-unblock purposes (COMPLETED, or FAILED on a HITL subtask title).
func ChildResolvedConditionSQL(alias string) (string, []any) {
	hitlClause, hitlArgs := HitlSubtaskTitleMatchSQL(alias + ".title")
	args := []any{models.TaskStateCompleted, models.TaskStateFailed}
	args = append(args, hitlArgs...)
	return fmt.Sprintf("(%s.state = ? OR (%s.state = ? AND %s))", alias, alias, hitlClause), args
}

// HitlSubtaskTitleMatchSQL returns a SQL LIKE clause for all HITL subtask title
// prefixes.
func HitlSubtaskTitleMatchSQL(titleExpr string) (string, []any) {
	parts := make([]string, len(models.HITLSubtaskTitlePrefixes))
	args := make([]any, len(models.HITLSubtaskTitlePrefixes))
	for i, prefix := range models.HITLSubtaskTitlePrefixes {
		parts[i] = titleExpr + " LIKE ?"
		args[i] = prefix + "%"
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// SelfHealingHandoffExcludeSQL matches models.IsSelfHealingHandoffTask for SQL filters.
func SelfHealingHandoffExcludeSQL() (string, []any) {
	return "NOT (assignee = ? AND title GLOB ?)",
		[]any{models.TaskAssigneeHuman, models.HITLSubtaskTitleManualReview + "*"}
}
