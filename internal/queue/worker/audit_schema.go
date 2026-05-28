package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	recordTypeToolDispatch = "tool_dispatch"
	recordTypeTurnSnapshot = "turn_snapshot"
	recordTypeHistoryEdit  = "history_edit"
	recordTypeTaskStart    = "task_start"
	recordTypeTaskComplete = "task_complete"
	recordTypeTaskFail     = "task_fail"
	recordTypeTaskReview   = "task_review"
	recordTypeDaemonStart  = "daemon_start"
)

// AuditRecord is a structured audit entry for a single tool dispatch.
// Arguments are hashed rather than stored in full to avoid leaking secrets.
type AuditRecord struct {
	Type             string    `json:"type,omitempty"`
	RecordType       string    `json:"record_type,omitempty"`
	TaskID           string    `json:"task_id,omitempty"`
	ProjectID        string    `json:"project_id,omitempty"`
	Provider         string    `json:"provider,omitempty"`
	TokenUsage       int       `json:"token_usage"`
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
	Type           string    `json:"type,omitempty"`
	RecordType     string    `json:"record_type,omitempty"`
	TaskID         string    `json:"task_id,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	TokenUsage     int       `json:"token_usage"`
	SessionID      string    `json:"session_id"`
	TurnID         string    `json:"turn_id"`
	TurnIndex      int       `json:"turn_index"`
	CheckpointID   string    `json:"checkpoint_id"`
	MessagesBefore int       `json:"messages_before"`
	MessagesAfter  int       `json:"messages_after"`
	NewContentHash string    `json:"new_content_hash"`
	Timestamp      time.Time `json:"timestamp"`
}

// TurnSnapshotRecord captures lightweight context metadata at a turn boundary
// to support session replay reconstruction without storing full context.
type TurnSnapshotRecord struct {
	Type         string    `json:"type,omitempty"`
	RecordType   string    `json:"record_type,omitempty"`
	TaskID       string    `json:"task_id,omitempty"`
	ProjectID    string    `json:"project_id,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	TokenUsage   int       `json:"token_usage"`
	SessionID    string    `json:"session_id"`
	TurnID       string    `json:"turn_id"`
	MessageCount int       `json:"message_count"`
	TokenCount   int       `json:"token_count"`
	ActiveTools  []string  `json:"active_tools"`
	GoalProgress float64   `json:"goal_progress"`
	Timestamp    time.Time `json:"timestamp"`
}

// TaskAuditRecord captures a legacy-mode task lifecycle event (start, complete, fail, or review handoff).
type TaskAuditRecord struct {
	Type       string    `json:"type,omitempty"`
	RecordType string    `json:"record_type,omitempty"`
	TaskID     string    `json:"task_id"`
	ProjectID  string    `json:"project_id"`
	Provider   string    `json:"provider"`
	Command    string    `json:"command,omitempty"`
	ExitCode   int       `json:"exit_code"`
	TokenUsage int       `json:"token_usage"`
	Timestamp  time.Time `json:"timestamp"`
}

// DaemonStartRecord is written to the audit file when the daemon initialises
// so that the presence of the file confirms audit is active.
type DaemonStartRecord struct {
	Type       string    `json:"type,omitempty"`
	RecordType string    `json:"record_type,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// AuditSink persists structured audit records.
type AuditSink interface {
	WriteAudit(AuditRecord) error
	WriteTurnSnapshot(TurnSnapshotRecord) error
	WriteHistoryEdit(HistoryEditRecord) error
	WriteTaskEvent(TaskAuditRecord) error
	WriteDaemonStart(DaemonStartRecord) error
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
	rec.Type = rec.RecordType
	if rec.TaskID == "" {
		rec.TaskID = rec.SessionID
	}
	return s.appendJSON(rec)
}

func (s *FileAuditSink) WriteTurnSnapshot(rec TurnSnapshotRecord) error {
	rec.RecordType = recordTypeTurnSnapshot
	rec.Type = rec.RecordType
	if rec.TaskID == "" {
		rec.TaskID = rec.SessionID
	}
	if rec.TokenUsage <= 0 {
		rec.TokenUsage = rec.TokenCount
	}
	return s.appendJSON(rec)
}

func (s *FileAuditSink) WriteHistoryEdit(rec HistoryEditRecord) error {
	rec.RecordType = recordTypeHistoryEdit
	rec.Type = rec.RecordType
	if rec.TaskID == "" {
		rec.TaskID = rec.SessionID
	}
	return s.appendJSON(rec)
}

func (s *FileAuditSink) WriteTaskEvent(rec TaskAuditRecord) error {
	if rec.RecordType == "" {
		rec.RecordType = rec.Type
	}
	if rec.Type == "" {
		rec.Type = rec.RecordType
	}
	return s.appendJSON(rec)
}

func (s *FileAuditSink) WriteDaemonStart(rec DaemonStartRecord) error {
	if rec.RecordType == "" {
		rec.RecordType = recordTypeDaemonStart
	}
	if rec.Type == "" {
		rec.Type = rec.RecordType
	}
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

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	if _, err := f.Write(data); err != nil {
		return err
	}
	return nil
}

// EnsureAuditFile creates the audit file (and parent directories) and writes a
// daemon_start marker so that the file exists before any task runs. It is safe
// to call multiple times; each call appends one daemon_start record.
func EnsureAuditFile(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	sink := NewFileAuditSink(path)
	rec := DaemonStartRecord{
		Type:       recordTypeDaemonStart,
		RecordType: recordTypeDaemonStart,
		Timestamp:  time.Now().UTC(),
	}
	return sink.WriteDaemonStart(rec)
}

func hashArgs(args string) string {
	sum := sha256.Sum256([]byte(args))
	return hex.EncodeToString(sum[:])
}

func nonNegativeDelta(after, before int) int {
	if after <= before {
		return 0
	}
	return after - before
}
