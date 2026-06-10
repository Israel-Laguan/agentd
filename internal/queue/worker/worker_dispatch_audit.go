package worker

import (
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
)

func (w *Worker) finalizeDispatchAudit(hookCtx agenthooks.HookContext, tr agenttools.ToolResult) {
	if w.budgetTracker != nil && hookCtx.SessionID != "" {
		hookCtx.TokenCountAfter = w.budgetTracker.Usage(hookCtx.SessionID)
	}
	var verdicts []string
	if hookCtx.Verdicts != nil {
		verdicts = *hookCtx.Verdicts
	}
	w.recordToolDispatch(hookCtx, tr, verdicts)
}

func (w *Worker) recordToolDispatch(hookCtx agenthooks.HookContext, tr agenttools.ToolResult, verdicts []string) {
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
	w.auditLogger.RecordTurnSnapshot(agentruntime.TurnSnapshotRecord{
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
