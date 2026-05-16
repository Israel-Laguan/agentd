package models

import "strings"

// HITLExpiresAtCommentPrefix is stored in worker-agent comment bodies so the
// kanban HITL reconcile loop can read expiry from COMMENT events (payload is
// "author: body"; kanban strips the author via splitCommentPayload).
const HITLExpiresAtCommentPrefix = "agentd:hitl:expires-at:"

// HITL subtask title prefixes for HUMAN subtasks created by worker handoffs
// and approval gates. Keep in sync with subtask titles in internal/queue/worker.
const (
	HITLSubtaskTitleApproveTool   = "Approve tool call: "
	HITLSubtaskTitleReview        = "Review required:"
	HITLSubtaskTitleClarification = "Clarification required: "
	HITLSubtaskTitleManualReview  = "Manual review required:"
	HITLSubtaskTitleManualAction  = "Manual action required:"
)

// HITLSubtaskTitlePrefixes lists all known HITL subtask title prefixes.
var HITLSubtaskTitlePrefixes = []string{
	HITLSubtaskTitleApproveTool,
	HITLSubtaskTitleReview,
	HITLSubtaskTitleClarification,
	HITLSubtaskTitleManualReview,
	HITLSubtaskTitleManualAction,
}

// IsHITLSubtaskTitle reports whether title was created by a HITL handoff or gate.
func IsHITLSubtaskTitle(title string) bool {
	for _, prefix := range HITLSubtaskTitlePrefixes {
		if strings.HasPrefix(title, prefix) {
			return true
		}
	}
	return false
}
