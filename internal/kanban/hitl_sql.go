package kanban

import (
	"fmt"
	"strings"

	"agentd/internal/models"
)

// childResolvedConditionSQL returns SQL for whether a child is resolved for
// parent-unblock purposes (COMPLETED, or FAILED on a HITL subtask title).
func childResolvedConditionSQL(alias string) (string, []any) {
	hitlClause, hitlArgs := hitlSubtaskTitleMatchSQL(alias + ".title")
	args := []any{models.TaskStateCompleted, models.TaskStateFailed}
	args = append(args, hitlArgs...)
	return fmt.Sprintf("(%s.state = ? OR (%s.state = ? AND %s))", alias, alias, hitlClause), args
}

func hitlSubtaskTitleMatchSQL(titleExpr string) (string, []any) {
	parts := make([]string, len(models.HITLSubtaskTitlePrefixes))
	args := make([]any, len(models.HITLSubtaskTitlePrefixes))
	for i, prefix := range models.HITLSubtaskTitlePrefixes {
		parts[i] = titleExpr + " LIKE ?"
		args[i] = prefix + "%"
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// selfHealingHandoffExcludeSQL matches models.IsSelfHealingHandoffTask for SQL filters.
func selfHealingHandoffExcludeSQL() (string, []any) {
	return "NOT (assignee = ? AND title LIKE ?)",
		[]any{models.TaskAssigneeHuman, models.HITLSubtaskTitleManualReview + "%"}
}
