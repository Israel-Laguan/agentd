package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"agentd/internal/agent/hooks"
	"agentd/internal/agent/tools"
)

func TestHashArgs_Deterministic(t *testing.T) {
	t.Parallel()
	args := `{"command":"ls -la","token":"sk-secret"}`
	a := HashArgs(args)
	b := HashArgs(args)
	if a != b {
		t.Fatalf("hashArgs not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(a))
	}
	if a == args {
		t.Fatal("hash should not equal raw args")
	}
}

func TestFileAuditSink_CreatesParentDir(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	path := filepath.Join(base, "nested", "dir", "audit.jsonl")
	if err := EnsureAuditFile(path); err != nil {
		t.Fatalf("EnsureAuditFile: %v", err)
	}
	sink := NewFileAuditSink(path)
	if err := sink.WriteAudit(AuditRecord{ToolName: "bash", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("audit file not created: %v", err)
	}
}

func TestFileAuditSink_ToolRecord(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit.jsonl")
	sink := NewFileAuditSink(path)
	rec := AuditRecord{
		SessionID:    "sess-1",
		TurnID:       "sess-1:0",
		ToolName:     "bash",
		ArgsHash:     HashArgs(`{"command":"echo hi"}`),
		ResultStatus: "success",
		ElapsedMs:    12,
		Timestamp:    time.Now().UTC(),
	}
	if err := sink.WriteAudit(rec); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat audit file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("audit file mode = %o, want 0600", info.Mode().Perm())
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	line := string(data)
	if !strings.Contains(line, `"args_hash"`) {
		t.Fatalf("missing args_hash in %q", line)
	}
	if strings.Contains(line, `"echo hi"`) {
		t.Fatalf("raw args must not appear in audit log: %q", line)
	}
	if strings.Contains(line, `"arguments"`) {
		t.Fatalf("raw arguments field must not appear: %q", line)
	}
}

func TestRecordToolDispatch_RawArgsNotInLog(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-secret.jsonl")
	secret := "sk-live-super-secret-token-abc123"
	logger := NewAuditLogger(NewFileAuditSink(path), true)

	ctx := hooks.HookContext{
		ToolName:  "bash",
		Args:      `{"api_key":"` + secret + `"}`,
		SessionID: "sess-secret",
		TurnID:    "sess-secret:1",
		Timestamp: time.Now(),
	}
	logger.RecordToolDispatch(ctx, tools.SuccessResult("c1", "ok", 1), nil, 0)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	content := string(data)
	if strings.Contains(content, secret) {
		t.Fatalf("audit log must not contain raw secret, got %q", content)
	}
	if !strings.Contains(content, HashArgs(ctx.Args)) {
		t.Fatal("audit log should contain args hash")
	}
}

func TestRecordToolDispatch_IncludesHookVerdicts(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-verdicts.jsonl")
	logger := NewAuditLogger(NewFileAuditSink(path), true)

	hc := hooks.NewHookChain()
	hc.RegisterPre(hooks.CredentialDetectionHook())
	var verdicts []string
	ctx := hooks.HookContext{
		ToolName:  "bash",
		Args:      `{"command":"curl -H 'Authorization: Bearer sk-abcdefghijklmnopqrstuvwxyz1234567890"}`,
		SessionID: "sess-v",
		TurnID:    "sess-v:0",
		Timestamp: time.Now(),
		Verdicts:  &verdicts,
	}
	_ = hc.RunPre(ctx)
	logger.RecordToolDispatch(ctx, tools.VetoedResult("c1", "blocked"), verdicts, 0)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	verdictList, ok := rec["hook_verdicts"].([]any)
	if !ok || len(verdictList) == 0 {
		t.Fatalf("hook_verdicts = %v, want non-empty", rec["hook_verdicts"])
	}
	found := false
	for _, v := range verdictList {
		if s, _ := v.(string); strings.HasPrefix(s, "credential-detection:veto") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected credential-detection:veto in %v", verdictList)
	}
}

func TestRecordTurnSnapshot_Metadata(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-turn.jsonl")
	logger := NewAuditLogger(NewFileAuditSink(path), true)

	logger.RecordTurnSnapshot(TurnSnapshotRecord{
		SessionID:    "task-1",
		TurnID:       "task-1:2",
		MessageCount: 7,
		TokenCount:   1500,
		ActiveTools:  []string{"bash", "read"},
		GoalProgress: 0.5,
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec["record_type"] != RecordTypeTurnSnapshot {
		t.Fatalf("record_type = %v, want %q", rec["record_type"], RecordTypeTurnSnapshot)
	}
	if rec["type"] != RecordTypeTurnSnapshot {
		t.Fatalf("type = %v, want %q", rec["type"], RecordTypeTurnSnapshot)
	}
	if rec["task_id"] != "task-1" {
		t.Fatalf("task_id = %v, want task-1", rec["task_id"])
	}
	if rec["token_usage"] == nil {
		t.Fatal("token_usage missing")
	}
	if int(rec["message_count"].(float64)) != 7 {
		t.Fatalf("message_count = %v, want 7", rec["message_count"])
	}
	if int(rec["token_count"].(float64)) != 1500 {
		t.Fatalf("token_count = %v, want 1500", rec["token_count"])
	}
	toolsList, ok := rec["active_tools"].([]any)
	if !ok || len(toolsList) != 2 {
		t.Fatalf("active_tools = %v, want [bash read]", rec["active_tools"])
	}
	if rec["goal_progress"].(float64) != 0.5 {
		t.Fatalf("goal_progress = %v, want 0.5", rec["goal_progress"])
	}
}

func TestFileAuditSink_HistoryEdit(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-edit.jsonl")
	sink := NewFileAuditSink(path)
	rec := HistoryEditRecord{
		SessionID:      "sess-edit",
		TurnID:         "sess-edit:0",
		TurnIndex:      -1,
		CheckpointID:   "sess-edit:cp:1",
		MessagesBefore: 5,
		MessagesAfter:  2,
		NewContentHash: HashArgs("revised"),
		Timestamp:      time.Now().UTC(),
	}
	if err := sink.WriteHistoryEdit(rec); err != nil {
		t.Fatalf("WriteHistoryEdit: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["record_type"] != RecordTypeHistoryEdit {
		t.Fatalf("record_type = %v, want %q", parsed["record_type"], RecordTypeHistoryEdit)
	}
	if parsed["type"] != RecordTypeHistoryEdit {
		t.Fatalf("type = %v, want %q", parsed["type"], RecordTypeHistoryEdit)
	}
	if parsed["task_id"] != "sess-edit" {
		t.Fatalf("task_id = %v, want sess-edit", parsed["task_id"])
	}
	if parsed["checkpoint_id"] != "sess-edit:cp:1" {
		t.Fatalf("checkpoint_id = %v", parsed["checkpoint_id"])
	}
	if strings.Contains(string(data), "revised") {
		t.Fatal("audit must not store raw revised content")
	}
}

func TestStructuredAuditHook_PassThrough(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-hook.jsonl")
	logger := NewAuditLogger(NewFileAuditSink(path), true)
	hook := StructuredAuditHook(logger)

	var verdicts []string
	verdicts = append(verdicts, "scrub_result:pass")
	ctx := hooks.HookContext{
		ToolName:         "bash",
		Args:             `{}`,
		SessionID:        "s1",
		TurnID:           "s1:0",
		Timestamp:        time.Now().Add(-10 * time.Millisecond),
		ResultStatus:     hooks.ToolStatusSuccess,
		ResultStatusSet:  true,
		Verdicts:         &verdicts,
		TokenCountBefore: 100,
		TokenCountAfter:  100,
	}
	got, err := hook.Fn(ctx, "result body")
	if err != nil {
		t.Fatalf("hook error: %v", err)
	}
	if got != "result body" {
		t.Fatalf("hook mutated result: %q", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("audit file not written: %v", err)
	}
}

func filepathJoinTemp(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func TestEnsureAuditFile_CreatesFile(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit.jsonl")
	if err := EnsureAuditFile(path); err != nil {
		t.Fatalf("EnsureAuditFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec["record_type"] != RecordTypeDaemonStart {
		t.Fatalf("record_type = %v, want %q", rec["record_type"], RecordTypeDaemonStart)
	}
	if rec["type"] != RecordTypeDaemonStart {
		t.Fatalf("type = %v, want %q", rec["type"], RecordTypeDaemonStart)
	}
	if _, ok := rec["timestamp"]; !ok {
		t.Fatal("missing timestamp field")
	}
}

func TestEnsureAuditFile_CreatesParentDirs(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "sub", "audit.jsonl")
	if err := EnsureAuditFile(path); err != nil {
		t.Fatalf("EnsureAuditFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestEnsureAuditFile_AppendsOnRepeat(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-repeat.jsonl")
	if err := EnsureAuditFile(path); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := EnsureAuditFile(path); err != nil {
		t.Fatalf("second call: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 daemon_start lines, got %d: %q", len(lines), string(data))
	}
	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d: unmarshal: %v", i, err)
		}
		if rec["record_type"] != RecordTypeDaemonStart {
			t.Fatalf("line %d: record_type = %v", i, rec["record_type"])
		}
		if rec["type"] != RecordTypeDaemonStart {
			t.Fatalf("line %d: type = %v", i, rec["type"])
		}
	}
}

func TestFileAuditSink_WriteTaskEvent(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-task.jsonl")
	sink := NewFileAuditSink(path)

	start := TaskAuditRecord{
		RecordType: RecordTypeTaskStart,
		TaskID:     "task-42",
		ProjectID:  "proj-1",
		Provider:   "openai",
		Timestamp:  time.Now().UTC(),
	}
	if err := sink.WriteTaskEvent(start); err != nil {
		t.Fatalf("WriteTaskEvent(start): %v", err)
	}
	complete := TaskAuditRecord{
		RecordType: RecordTypeTaskComplete,
		TaskID:     "task-42",
		ProjectID:  "proj-1",
		Provider:   "openai",
		Command:    "echo done",
		ExitCode:   0,
		TokenUsage: 150,
		Timestamp:  time.Now().UTC(),
	}
	if err := sink.WriteTaskEvent(complete); err != nil {
		t.Fatalf("WriteTaskEvent(complete): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	for i, wantType := range []string{RecordTypeTaskStart, RecordTypeTaskComplete} {
		var rec map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &rec); err != nil {
			t.Fatalf("line %d unmarshal: %v", i, err)
		}
		assertTaskRecordCoreFields(t, rec, i, wantType)
	}
	assertCompleteRecordFields(t, lines[1])
}

func TestAuditLogger_RecordTaskEvent_NilSafe(t *testing.T) {
	t.Parallel()
	var l *AuditLogger
	l.RecordTaskEvent(TaskAuditRecord{RecordType: RecordTypeTaskStart, TaskID: "x"})
	l.RecordDaemonStart()
}

func assertTaskRecordCoreFields(t *testing.T, rec map[string]any, line int, wantType string) {
	t.Helper()
	if rec["record_type"] != wantType {
		t.Fatalf("line %d: record_type = %v, want %q", line, rec["record_type"], wantType)
	}
	if rec["type"] != wantType {
		t.Fatalf("line %d: type = %v, want %q", line, rec["type"], wantType)
	}
	if rec["task_id"] != "task-42" {
		t.Fatalf("line %d: task_id = %v", line, rec["task_id"])
	}
	if rec["project_id"] != "proj-1" {
		t.Fatalf("line %d: project_id = %v", line, rec["project_id"])
	}
}

func assertCompleteRecordFields(t *testing.T, line string) {
	t.Helper()
	var completeRec map[string]any
	if err := json.Unmarshal([]byte(line), &completeRec); err != nil {
		t.Fatalf("complete unmarshal: %v", err)
	}
	if completeRec["command"] != "echo done" {
		t.Fatalf("command = %v", completeRec["command"])
	}
	if int(completeRec["token_usage"].(float64)) != 150 {
		t.Fatalf("token_usage = %v", completeRec["token_usage"])
	}
}
