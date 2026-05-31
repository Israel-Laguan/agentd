package runtime

import (
	"log/slog"
	"time"

	"agentd/internal/agent/hooks"
	"agentd/internal/agent/tools"
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

// Enabled reports whether structured audit logging is active.
func (l *AuditLogger) Enabled() bool {
	return l != nil && l.enabled && l.sink != nil
}

// RecordToolDispatch writes a tool dispatch audit record.
func (l *AuditLogger) RecordToolDispatch(ctx hooks.HookContext, tr tools.ToolResult, verdicts []string, tokenAfter int) {
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
		ArgsHash:         HashArgs(ctx.Args),
		ResultStatus:     status,
		ElapsedMs:        elapsed,
		HookVerdicts:     append([]string(nil), verdicts...),
		TokenCountBefore: ctx.TokenCountBefore,
		TokenCountAfter:  tokenAfter,
		Timestamp:        time.Now().UTC(),
	}
	if err := l.sink.WriteAudit(rec); err != nil {
		slog.Warn("structured audit write failed",
			"record_type", RecordTypeToolDispatch,
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
			"record_type", RecordTypeHistoryEdit,
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
			"record_type", RecordTypeTurnSnapshot,
			"session_id", rec.SessionID,
			"turn_id", rec.TurnID,
			"error", err,
		)
	}
}

// RecordTaskEvent writes a task lifecycle event (task_start, task_complete, task_fail, or task_review).
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
		Type:       RecordTypeDaemonStart,
		RecordType: RecordTypeDaemonStart,
		Timestamp:  time.Now().UTC(),
	}
	if err := l.sink.WriteDaemonStart(rec); err != nil {
		slog.Warn("structured audit write failed", "record_type", RecordTypeDaemonStart, "error", err)
	}
}

// StructuredAuditHook returns a PostHook that records a tool dispatch audit entry.
// Production dispatch uses recordToolDispatch after the full hook chain instead;
// this hook exists for isolated unit tests.
func StructuredAuditHook(logger *AuditLogger) hooks.PostHook {
	return hooks.PostHook{
		Name:   "structured_audit",
		Policy: hooks.FailOpen,
		Fn: func(ctx hooks.HookContext, result string) (string, error) {
			if logger == nil || !logger.Enabled() {
				return result, nil
			}
			tr := tools.ToolResult{Content: result}
			if ctx.ResultStatusSet {
				tr.Status = tools.ToolStatus(ctx.ResultStatus)
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

func auditResultStatus(ctx hooks.HookContext, tr tools.ToolResult) string {
	if ctx.ResultStatusSet {
		return ctx.ResultStatus.String()
	}
	if tr.Status != 0 {
		return tr.Status.String()
	}
	return tools.ToolStatusSuccess.String()
}
