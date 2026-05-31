package worker

import (
	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
)

func (w *Worker) recordToolDispatch(hookCtx HookContext, tr ToolResult, verdicts []string) {
	if w.auditLogger == nil || !w.auditLogger.Enabled() {
		return
	}
	w.auditLogger.RecordToolDispatch(
		agenthooks.HookContext(hookCtx),
		agenttools.ToolResult(tr),
		verdicts,
		hookCtx.TokenCountAfter,
	)
}

func (w *Worker) recordTurnSnapshot(
	sessionID, projectID, provider, turnID string,
	messageCount, tokenCount int,
	activeTools []string,
	goalProgress float64,
) {
	if w.auditLogger == nil || !w.auditLogger.Enabled() {
		return
	}
	w.auditLogger.RecordTurnSnapshot(TurnSnapshotRecord{
		TaskID:       sessionID,
		ProjectID:    projectID,
		Provider:     provider,
		TokenUsage:   tokenCount,
		SessionID:    sessionID,
		TurnID:       turnID,
		MessageCount: messageCount,
		TokenCount:   tokenCount,
		ActiveTools:  append([]string(nil), activeTools...),
		GoalProgress: goalProgress,
	})
}

func toolNamesFromDefinitions(tools []gateway.ToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}
