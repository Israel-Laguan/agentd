package worker

import (
	"log/slog"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
)

// AuditLogger writes structured audit records when enabled.
type AuditLogger struct {
	sink    AuditSink
	enabled bool
}

// NewAuditLogger returns a logger backed by sink. When enabled is false, writes are no-ops.
func NewAuditLogger(sink AuditSink, enabled bool) *AuditLogger {
	return &AuditLogger{sink: sink, enabled: enabled}
}

func newAuditLogger(cfg config.AuditConfig) *AuditLogger {
	if !cfg.Enabled || cfg.Path == "" {
		return nil
	}
	return NewAuditLogger(NewFileAuditSink(cfg.Path), true)
}

// Enabled reports whether structured audit logging is active.
func (l *AuditLogger) Enabled() bool {
	return l != nil && l.enabled && l.sink != nil
}

// RecordToolDispatch writes a tool dispatch audit record.
func (l *AuditLogger) RecordToolDispatch(ctx HookContext, tr ToolResult, verdicts []string, tokenAfter int) {
	if !l.Enabled() {
		return
	}
	elapsed := time.Since(ctx.Timestamp).Milliseconds()
	if tr.ElapsedMs > 0 {
		elapsed = tr.ElapsedMs
	}
	status := auditResultStatus(ctx, tr)
	rec := AuditRecord{
		TaskID:           ctx.SessionID,
		ProjectID:        ctx.ProjectID,
		Provider:         ctx.Provider,
		TokenUsage:       nonNegativeDelta(tokenAfter, ctx.TokenCountBefore),
		SessionID:        ctx.SessionID,
		TurnID:           ctx.TurnID,
		ToolName:         ctx.ToolName,
		ArgsHash:         hashArgs(ctx.Args),
		ResultStatus:     status,
		ElapsedMs:        elapsed,
		HookVerdicts:     append([]string(nil), verdicts...),
		TokenCountBefore: ctx.TokenCountBefore,
		TokenCountAfter:  tokenAfter,
		Timestamp:        time.Now().UTC(),
	}
	if err := l.sink.WriteAudit(rec); err != nil {
		slog.Warn("structured audit write failed",
			"record_type", recordTypeToolDispatch,
			"session_id", rec.SessionID,
			"turn_id", rec.TurnID,
			"tool_name", rec.ToolName,
			"error", err,
		)
	}
}

// RecordHistoryEdit writes a history edit audit record.
func (l *AuditLogger) RecordHistoryEdit(rec HistoryEditRecord) {
	if !l.Enabled() {
		return
	}
	rec.Timestamp = time.Now().UTC()
	if err := l.sink.WriteHistoryEdit(rec); err != nil {
		slog.Warn("structured audit write failed",
			"record_type", recordTypeHistoryEdit,
			"session_id", rec.SessionID,
			"turn_id", rec.TurnID,
			"error", err,
		)
	}
}

// RecordTurnSnapshot writes a turn-boundary context snapshot.
func (l *AuditLogger) RecordTurnSnapshot(rec TurnSnapshotRecord) {
	if !l.Enabled() {
		return
	}
	rec.Timestamp = time.Now().UTC()
	if err := l.sink.WriteTurnSnapshot(rec); err != nil {
		slog.Warn("structured audit write failed",
			"record_type", recordTypeTurnSnapshot,
			"session_id", rec.SessionID,
			"turn_id", rec.TurnID,
			"error", err,
		)
	}
}

// RecordTaskEvent writes a task lifecycle event (task_start, task_complete, or task_fail).
func (l *AuditLogger) RecordTaskEvent(rec TaskAuditRecord) {
	if !l.Enabled() {
		return
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}
	if rec.Type == "" {
		rec.Type = rec.RecordType
	}
	if err := l.sink.WriteTaskEvent(rec); err != nil {
		slog.Warn("structured audit write failed",
			"record_type", rec.RecordType,
			"task_id", rec.TaskID,
			"error", err,
		)
	}
}

// RecordDaemonStart writes a daemon_start marker to the audit file.
func (l *AuditLogger) RecordDaemonStart() {
	if !l.Enabled() {
		return
	}
	rec := DaemonStartRecord{
		Type:       recordTypeDaemonStart,
		RecordType: recordTypeDaemonStart,
		Timestamp:  time.Now().UTC(),
	}
	if err := l.sink.WriteDaemonStart(rec); err != nil {
		slog.Warn("structured audit write failed", "record_type", recordTypeDaemonStart, "error", err)
	}
}

// StructuredAuditHook returns a PostHook that records a tool dispatch audit entry.
// Production dispatch uses recordToolDispatch after the full hook chain instead;
// this hook exists for isolated unit tests.
func StructuredAuditHook(logger *AuditLogger) PostHook {
	return PostHook{
		Name:   "structured_audit",
		Policy: FailOpen,
		Fn: func(ctx HookContext, result string) (string, error) {
			if logger == nil || !logger.Enabled() {
				return result, nil
			}
			tr := ToolResult{Content: result}
			if ctx.ResultStatusSet {
				tr.Status = ctx.ResultStatus
			}
			var verdicts []string
			if ctx.Verdicts != nil {
				verdicts = *ctx.Verdicts
			}
			logger.RecordToolDispatch(ctx, tr, verdicts, ctx.TokenCountAfter)
			return result, nil
		},
	}
}

func auditResultStatus(ctx HookContext, tr ToolResult) string {
	if ctx.ResultStatusSet {
		return ctx.ResultStatus.String()
	}
	if tr.Status != 0 {
		return tr.Status.String()
	}
	return ToolStatusSuccess.String()
}

func (w *Worker) recordToolDispatch(hookCtx HookContext, tr ToolResult, verdicts []string) {
	if w.auditLogger == nil || !w.auditLogger.Enabled() {
		return
	}
	w.auditLogger.RecordToolDispatch(hookCtx, tr, verdicts, hookCtx.TokenCountAfter)
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
