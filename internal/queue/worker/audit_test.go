package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
)

func TestHashArgs_Deterministic(t *testing.T) {
	t.Parallel()
	args := `{"command":"ls -la","token":"sk-secret"}`
	a := hashArgs(args)
	b := hashArgs(args)
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

func TestFileAuditSink_ToolRecord(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit.jsonl")
	sink := NewFileAuditSink(path)
	rec := AuditRecord{
		SessionID:    "sess-1",
		TurnID:       "sess-1:0",
		ToolName:     "bash",
		ArgsHash:     hashArgs(`{"command":"echo hi"}`),
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
	// Production requests 0600; Windows ACLs are not reflected in Mode().Perm().
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

	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"api_key":"` + secret + `"}`,
		SessionID: "sess-secret",
		TurnID:    "sess-secret:1",
		Timestamp: time.Now(),
	}
	logger.RecordToolDispatch(ctx, SuccessResult("c1", "ok", 1), nil, 0)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	content := string(data)
	if strings.Contains(content, secret) {
		t.Fatalf("audit log must not contain raw secret, got %q", content)
	}
	if !strings.Contains(content, hashArgs(ctx.Args)) {
		t.Fatal("audit log should contain args hash")
	}
}

func TestRecordToolDispatch_IncludesHookVerdicts(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-verdicts.jsonl")
	logger := NewAuditLogger(NewFileAuditSink(path), true)

	hc := NewHookChain()
	hc.RegisterPre(CredentialDetectionHook())
	var verdicts []string
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"curl -H 'Authorization: Bearer sk-abcdefghijklmnopqrstuvwxyz1234567890"}`,
		SessionID: "sess-v",
		TurnID:    "sess-v:0",
		Timestamp: time.Now(),
		Verdicts:  &verdicts,
	}
	_ = hc.RunPre(ctx)
	logger.RecordToolDispatch(ctx, VetoedResult("c1", "blocked"), verdicts, 0)

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
	if rec["record_type"] != recordTypeTurnSnapshot {
		t.Fatalf("record_type = %v, want %q", rec["record_type"], recordTypeTurnSnapshot)
	}
	if int(rec["message_count"].(float64)) != 7 {
		t.Fatalf("message_count = %v, want 7", rec["message_count"])
	}
	if int(rec["token_count"].(float64)) != 1500 {
		t.Fatalf("token_count = %v, want 1500", rec["token_count"])
	}
	tools, ok := rec["active_tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("active_tools = %v, want [bash read]", rec["active_tools"])
	}
	if rec["goal_progress"].(float64) != 0.5 {
		t.Fatalf("goal_progress = %v, want 0.5", rec["goal_progress"])
	}
}

func TestDispatchToolWithHooks_WritesStructuredAudit(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-dispatch.jsonl")
	w := NewWorker(nil, nil, &fakeSuccessExecutor{}, nil, nil, WorkerOptions{
		MaxToolIterations: 5,
		Audit: config.AuditConfig{
			Enabled: true,
			Path:    path,
		},
	})

	call := gateway.ToolCall{
		ID: "call-audit-1",
		Function: gateway.ToolCallFunction{
			Name:      "bash",
			Arguments: `{"command":"echo audit"}`,
		},
	}
	_, _ = w.dispatchToolWithHooks(
		context.Background(), "sess-d", "proj-d", "sess-d:0", time.Now(),
		call, nil, w.toolExecutor, nil, nil,
	)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if strings.Contains(string(data), "echo audit") {
		t.Fatal("raw args must not appear in structured audit log")
	}
	if !strings.Contains(string(data), `"record_type":"tool_dispatch"`) &&
		!strings.Contains(string(data), `"record_type": "tool_dispatch"`) {
		t.Fatalf("expected tool_dispatch record, got %q", string(data))
	}
}

func TestStructuredAuditHook_PassThrough(t *testing.T) {
	t.Parallel()
	path := filepathJoinTemp(t, "audit-hook.jsonl")
	logger := NewAuditLogger(NewFileAuditSink(path), true)
	hook := StructuredAuditHook(logger)

	var verdicts []string
	verdicts = append(verdicts, "scrub_result:pass")
	ctx := HookContext{
		ToolName:         "bash",
		Args:             `{}`,
		SessionID:        "s1",
		TurnID:           "s1:0",
		Timestamp:        time.Now().Add(-10 * time.Millisecond),
		ResultStatus:     ToolStatusSuccess,
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
