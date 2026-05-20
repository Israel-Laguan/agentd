package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
)

const (
	recordTypeToolDispatch = "tool_dispatch"
	recordTypeTurnSnapshot = "turn_snapshot"
	recordTypeHistoryEdit  = "history_edit"
)

// AuditRecord is a structured audit entry for a single tool dispatch.
// Arguments are hashed rather than stored in full to avoid leaking secrets.
type AuditRecord struct {
	RecordType       string    `json:"record_type"`
	SessionID        string    `json:"session_id"`
	TurnID           string    `json:"turn_id"`
	ToolName         string    `json:"tool_name"`
	ArgsHash         string    `json:"args_hash"`
	ResultStatus     string    `json:"result_status"`
	ElapsedMs        int64     `json:"elapsed_ms"`
	HookVerdicts     []string  `json:"hook_verdicts"`
	TokenCountBefore int       `json:"token_count_before"`
	TokenCountAfter  int       `json:"token_count_after"`
	Timestamp        time.Time `json:"timestamp"`
}

// HistoryEditRecord captures a turn-level history rewrite for observability.
// New content is hashed rather than stored in full to avoid leaking secrets.
type HistoryEditRecord struct {
	RecordType      string    `json:"record_type"`
	SessionID       string    `json:"session_id"`
	TurnID          string    `json:"turn_id"`
	TurnIndex       int       `json:"turn_index"`
	CheckpointID    string    `json:"checkpoint_id"`
	MessagesBefore  int       `json:"messages_before"`
	MessagesAfter   int       `json:"messages_after"`
	NewContentHash  string    `json:"new_content_hash"`
	Timestamp       time.Time `json:"timestamp"`
}

// TurnSnapshotRecord captures lightweight context metadata at a turn boundary
// to support session replay reconstruction without storing full context.
type TurnSnapshotRecord struct {
	RecordType   string    `json:"record_type"`
	SessionID    string    `json:"session_id"`
	TurnID       string    `json:"turn_id"`
	MessageCount int       `json:"message_count"`
	TokenCount   int       `json:"token_count"`
	ActiveTools  []string  `json:"active_tools"`
	GoalProgress float64   `json:"goal_progress"`
	Timestamp    time.Time `json:"timestamp"`
}

// AuditSink persists structured audit records.
type AuditSink interface {
	WriteAudit(AuditRecord) error
	WriteTurnSnapshot(TurnSnapshotRecord) error
	WriteHistoryEdit(HistoryEditRecord) error
}

// FileAuditSink appends JSON lines to a file.
type FileAuditSink struct {
	path string
	mu   sync.Mutex
}

// NewFileAuditSink returns a sink that writes to path.
func NewFileAuditSink(path string) *FileAuditSink {
	return &FileAuditSink{path: path}
}

func (s *FileAuditSink) WriteAudit(rec AuditRecord) error {
	rec.RecordType = recordTypeToolDispatch
	return s.appendJSON(rec)
}

func (s *FileAuditSink) WriteTurnSnapshot(rec TurnSnapshotRecord) error {
	rec.RecordType = recordTypeTurnSnapshot
	return s.appendJSON(rec)
}

func (s *FileAuditSink) WriteHistoryEdit(rec HistoryEditRecord) error {
	rec.RecordType = recordTypeHistoryEdit
	return s.appendJSON(rec)
}

func (s *FileAuditSink) appendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

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

func hashArgs(args string) string {
	sum := sha256.Sum256([]byte(args))
	return hex.EncodeToString(sum[:])
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

func (w *Worker) recordTurnSnapshot(sessionID, turnID string, messageCount, tokenCount int, activeTools []string, goalProgress float64) {
	if w.auditLogger == nil || !w.auditLogger.Enabled() {
		return
	}
	w.auditLogger.RecordTurnSnapshot(TurnSnapshotRecord{
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
